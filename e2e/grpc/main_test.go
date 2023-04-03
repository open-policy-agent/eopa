//go:build e2e

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestGRPCSmokeTest(t *testing.T) {
	data := `{}`
	policy := `package test
import future.keywords
p if rand.intn("coin", 2) == 0
`
	ctx := context.Background()

	load, loadOut := loadRun(t, policy, data, "--set", "plugins.grpc.addr=localhost:9090")
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	waitForLog(ctx, t, loadOut, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 3; i++ {
		if err := grpcurlSimple("-plaintext", "localhost:9090", "list"); err != nil {
			if i == 2 {
				t.Fatalf("wait for gRPC endpoint: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}
	}

	{
		out := grpcurl(t, "-d", `{"policy": {"path": "/test", "text": "package foo allow := x {x = true}"}}`, "-plaintext", "localhost:9090", "load.v1.PolicyService/CreatePolicy")
		var m map[string]any
		if err := json.NewDecoder(out).Decode(&m); err != nil {
			t.Fatal(err)
		}
		if exp, act := 0, len(m); exp != act {
			t.Fatalf("expected empty result %v, got %v", exp, m)
		}
	}
	{
		out := grpcurl(t, "-d", `{"path": "/foo"}`, "-plaintext", "localhost:9090", "load.v1.DataService/GetData")
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

func loadRun(t *testing.T, policy, data string, extraArgs ...string) (*exec.Cmd, *bytes.Buffer) {
	logLevel := "debug"
	buf := bytes.Buffer{}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
		t.Fatalf("write data: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", "localhost:38181",
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	args = append(args, extraArgs...)
	args = append(args,
		dataPath,
		policyPath,
	)
	load := exec.Command(binary(), args...)
	load.Stderr = &buf
	load.Env = append(load.Environ(),
		"STYRA_LOAD_LICENSE_TOKEN="+os.Getenv("STYRA_LOAD_LICENSE_TOKEN"),
		"STYRA_LOAD_LICENSE_KEY="+os.Getenv("STYRA_LOAD_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if load.Process == nil {
			return
		}
		if err := load.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		load.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("load output:\n%s", buf.String())
		}
	})

	return load, &buf
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

func grpcurlSimple(args ...string) error {
	_, err := exec.Command("grpcurl", args...).Output()
	return err
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func waitForLog(ctx context.Context, t *testing.T, rdr io.Reader, exp int, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(dur)
		}
		if act := retrieveMsg(ctx, t, rdr, assert); act == exp {
			return
		} else if i == 3 {
			t.Fatalf("expected %d requests, got %d", exp, act)
		}
	}
	return
}

func retrieveMsg(ctx context.Context, t *testing.T, rdr io.Reader, assert func(string) bool) int {
	t.Helper()
	c := 0
	// Skip any non-JSON logs coming from CLI arg validation
	buf := bytes.Buffer{}
	if _, err := io.Copy(&buf, rdr); err != nil {
		t.Fatal(err)
	}
	if _, err := buf.ReadBytes('{'); err == nil {
		if err := buf.UnreadByte(); err != nil {
			t.Fatal(err)
		}
	}

	dec := json.NewDecoder(&buf)
	for {
		m := struct {
			Msg string `json:"msg"`
		}{}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m.Msg) {
			c++
		}
	}
	return c
}
