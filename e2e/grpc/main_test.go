//go:build e2e

package grpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

func TestGRPCSmokeTest(t *testing.T) {
	data := `{}`
	policy := `package test
import future.keywords
p if rand.intn("coin", 2) == 0
`
	eopa, eopaOut := eopaRun(t, policy, data, "", "--set", "plugins.grpc.addr=localhost:9090")
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 3; i++ {
		if err := grpcurlSimpleCheck("-plaintext", "localhost:9090", "list"); err != nil {
			if i == 2 {
				t.Fatalf("wait for gRPC endpoint: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}
	}

	{
		out := grpcurl(t, "-d", `{"policy": {"path": "/test", "text": "package foo allow := x {x = true}"}}`, "-plaintext", "localhost:9090", "eopa.policy.v1.PolicyService/CreatePolicy")
		var m map[string]any
		if err := json.NewDecoder(out).Decode(&m); err != nil {
			t.Fatal(err)
		}
		if exp, act := 0, len(m); exp != act {
			t.Fatalf("expected empty result %v, got %v", exp, m)
		}
	}
	{
		out := grpcurl(t, "-d", `{"path": "/foo"}`, "-plaintext", "localhost:9090", "eopa.data.v1.DataService/GetData")
		var act map[string]any
		if err := json.NewDecoder(out).Decode(&act); err != nil {
			t.Fatal(err)
		}
		exp := map[string]any{
			"result": map[string]any{
				"document": map[string]any{"allow": true},
				"path":     "/foo",
			},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("unexpected result (-want, +got):\n%s", diff)
		}
	}
}

func TestGRPCBundleSmokeTest(t *testing.T) {
	policy := `package rules

main["allowed"] := allow

default allow := false

allow {
	input.user == "kurt"
}
`

	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.tar.gz")
	if err := createTarGzArchive(
		map[string]string{
			"policy.rego": policy,
			"data.json":   "{}",
		}, bundlePath); err != nil {
		t.Fatal(err)
	}

	eopa, eopaOut := eopaRun(t, "", "", "", "-b", bundlePath, "--set", "plugins.grpc.addr=localhost:9090")
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 3; i++ {
		if err := grpcurlSimpleCheck("-plaintext", "localhost:9090", "list"); err != nil {
			if i == 2 {
				t.Fatalf("wait for gRPC endpoint: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}
	}
	{
		out := grpcurl(t, "-d", `{"path": "/rules/main", "input": {"document": {"user":"kurt"}}}`, "-plaintext", "localhost:9090", "eopa.data.v1.DataService/GetData")
		var act map[string]any
		if err := json.NewDecoder(out).Decode(&act); err != nil {
			t.Fatal(err)
		}
		exp := map[string]any{
			"result": map[string]any{
				"document": map[string]any{"allowed": true},
				"path":     "/rules/main",
			},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("unexpected result (-want, +got):\n%s", diff)
		}
	}
}

func TestGRPCDecisionLogs(t *testing.T) {
	data := `{}`
	policy := `package test
import future.keywords
p if rand.intn("coin", 2) == 0
`
	config := `
distributed_tracing:
  type: grpc
  address: 127.0.0.1:4317
  sample_percentage: 100

decision_logs:
  console: true

plugins:
  grpc:
    addr: localhost:9090
`
	eopa, eopaOut := eopaRun(t, policy, data, config)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 3; i++ {
		if err := grpcurlSimpleCheck("-plaintext", "localhost:9090", "list"); err != nil {
			if i == 2 {
				t.Fatalf("wait for gRPC endpoint: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}
	}

	{
		_ = grpcurl(t, "-d", `{"policy": {"path": "/test", "text": "package foo allow := x {x = true}"}}`, "-plaintext", "localhost:9090", "eopa.policy.v1.PolicyService/CreatePolicy")
		// "msg":"Received request.", "req_id":5, "req_method":"/eopa.policy.v1.PolicyService/CreatePolicy"
		wait.ForLogFields(t, eopaOut, func(m map[string]any) bool {
			return fieldContainsString(m, "msg", "Received request.") &&
				fieldEqualsInt(m, "req_id", 5) &&
				fieldContainsString(m, "req_method", "/eopa.policy.v1.PolicyService/CreatePolicy")
		}, time.Second)
		// "msg":"Sent response.", "req_id":5, "req_method":"/eopa.policy.v1.PolicyService/CreatePolicy"
		wait.ForLogFields(t, eopaOut, func(m map[string]any) bool {
			return fieldContainsString(m, "msg", "Sent response.") &&
				fieldEqualsInt(m, "req_id", 5) &&
				fieldContainsString(m, "req_method", "/eopa.policy.v1.PolicyService/CreatePolicy")
		}, time.Second)
	}
	{
		_ = grpcurl(t, "-d", `{"path": "/foo"}`, "-plaintext", "localhost:9090", "eopa.data.v1.DataService/GetData")
		// "msg":"Received request.", "req_id":7, "req_method":"/eopa.data.v1.DataService/GetData"
		wait.ForLogFields(t, eopaOut, func(m map[string]any) bool {
			return fieldContainsString(m, "msg", "Received request.") &&
				fieldEqualsInt(m, "req_id", 7) &&
				fieldContainsString(m, "req_method", "/eopa.data.v1.DataService/GetData")
		}, time.Second)
		// {"decision_id":"a5d51764-b1ce-4d80-8c0f-0e96b0ed4f04", "msg":"Decision Log", "path":"/foo", "type":"openpolicyagent.org/decision_logs"}
		wait.ForLogFields(t, eopaOut, func(m map[string]any) bool {
			return fieldContainsString(m, "msg", "Decision Log") &&
				fieldContainsString(m, "path", "/foo") &&
				fieldRegexMatch(m, "decision_id", `[[:alnum:]]{8}-[[:alnum:]]{4}-[[:alnum:]]{4}-[[:alnum:]]{4}-[[:alnum:]]{12}`) &&
				fieldContainsString(m, "type", "openpolicyagent.org/decision_logs") &&
				fieldRegexMatch(m, "trace_id", `[0-9a-f]{32}`) &&
				fieldRegexMatch(m, "span_id", `[0-9a-f]{16}`)
		}, time.Second)
		// "msg":"Sent response.", "req_id":7, "req_method":"/eopa.data.v1.DataService/GetData"
		wait.ForLogFields(t, eopaOut, func(m map[string]any) bool {
			return fieldContainsString(m, "msg", "Sent response.") &&
				fieldEqualsInt(m, "req_id", 7) &&
				fieldContainsString(m, "req_method", "/eopa.data.v1.DataService/GetData")
		}, time.Second)

	}
}

func eopaRun(t *testing.T, policy, data, config string, extraArgs ...string) (*exec.Cmd, *bytes.Buffer) {
	logLevel := "debug"
	buf := bytes.Buffer{}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "eval.rego")
	if policy != "" {
		if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
			t.Fatalf("write policy: %v", err)
		}
	}
	dataPath := filepath.Join(dir, "data.json")
	if data != "" {
		if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}
	configPath := filepath.Join(dir, "config.yaml")
	if config != "" {
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	args := []string{
		"run",
		"--server",
		"--addr", "localhost:38181",
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	if config != "" {
		args = append(args, "-c", configPath)
	}
	args = append(args, extraArgs...)
	// Note(philip): This hackery is required so that we can manually pass
	// in the arguments to load up a bundle from the CLI.
	if data != "" {
		args = append(args, dataPath)
	}
	if policy != "" {
		args = append(args, policyPath)
	}
	eopa := exec.Command(binary(), args...)
	eopa.Stderr = &buf
	eopa.Env = append(eopa.Environ(),
		"EOPA_LICENSE_TOKEN="+os.Getenv("EOPA_LICENSE_TOKEN"),
		"EOPA_LICENSE_KEY="+os.Getenv("EOPA_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa output:\n%s", buf.String())
		}
	})

	return eopa, &buf
}

func grpcurl(t *testing.T, args ...string) *bytes.Buffer {
	t.Helper()
	buf := bytes.Buffer{}
	c := exec.Command("grpcurl", args...)
	c.Stdout = &buf
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Wait(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func grpcurlSimpleCheck(args ...string) error {
	_, err := exec.Command("grpcurl", args...).Output()
	return err
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

func fieldContainsString(m map[string]any, key string, s string) bool {
	if v, ok := m[key]; ok {
		if strValue, ok := v.(string); ok {
			return strings.Contains(strValue, s)
		}
	}
	return false
}

func fieldEqualsInt(m map[string]any, key string, i int) bool {
	if v, ok := m[key]; ok {
		if n, ok := v.(float64); ok {
			return i == int(n)
		}
	}
	return false
}

func fieldRegexMatch(m map[string]any, key string, regexStr string) bool {
	if v, ok := m[key]; ok {
		if strValue, ok := v.(string); ok {
			matched, _ := regexp.MatchString(regexStr, strValue)
			return matched
		}
	}
	return false
}

// Note(philip): This function was generated by ChatGPT-3.5.
func createTarGzArchive(files map[string]string, archiveName string) error {
	// Create a new tar.gz file
	file, err := os.Create(archiveName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a gzip writer
	gw := gzip.NewWriter(file)
	defer gw.Close()

	// Create a tar writer
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for filename, content := range files {
		// Create a new tar header
		header := &tar.Header{
			Name: filename,
			Mode: 0o644, // Set appropriate file permissions here
			Size: int64(len(content)),
		}

		// Write the tar header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write the file content
		if _, err := io.WriteString(tw, content); err != nil {
			return err
		}
	}

	return nil
}
