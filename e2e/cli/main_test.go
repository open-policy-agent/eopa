//go:build e2e

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

func TestEvalInstructionLimit(t *testing.T) {
	data := largeJSON()

	t.Run("limit=1", func(t *testing.T) {
		eopa := eopaEval(t, "1", data)
		out, err := eopa.Output()
		if err == nil {
			t.Fatalf("expected error, got output: %s", string(out))
		}
		output := struct {
			Errors []struct {
				Message string
			}
		}{}
		if err := json.NewDecoder(bytes.NewReader(out)).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := 1, len(output.Errors); exp != act {
			t.Fatalf("expected %d errors, got %d", exp, act)
		}
		if exp, act := "instructions limit exceeded", output.Errors[0].Message; exp != act {
			t.Fatalf("expected message %q, got %q", exp, act)
		}
	})

	t.Run("limit=10000", func(t *testing.T) {
		eopa := eopaEval(t, "10000", data)
		_, err := eopa.Output()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestRunInstructionLimit(t *testing.T) {
	data := largeJSON()
	config := ``
	policy := `package test
p { data.xs[_] }`
	ctx := context.Background()

	t.Run("limit=1", func(t *testing.T) {
		eopa, eopaOut := eopaRun(t, policy, data, config, eopaHTTPPort, "--instruction-limit", "1")
		if err := eopa.Start(); err != nil {
			t.Fatal(err)
		}
		wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/p", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusInternalServerError, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Code    string
			Message string
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := "instructions limit exceeded", output.Message; exp != act {
			t.Fatalf("expected message %q, got %q", exp, act)
		}
		if exp, act := "internal_error", output.Code; exp != act {
			t.Fatalf("expected code %q, got %q", exp, act)
		}
	})

	t.Run("limit=10000", func(t *testing.T) {
		eopa, eopaOut := eopaRun(t, policy, data, config, eopaHTTPPort, "--instruction-limit", "10000")
		if err := eopa.Start(); err != nil {
			t.Fatal(err)
		}
		wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/p", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusOK, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Result any
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := true, output.Result; exp != act {
			t.Fatalf("expected result %v, got %v", exp, act)
		}
	})
}

// NOTE(sr): This isn't *all* plugins -- the data plugin isn't loading its sub-plugins.
// But it's most of them.
func TestRunWithAllPlugins(t *testing.T) {
	policy := `package test`
	data := `{}`
	config := `
plugins:
  impact_analysis: {}
  grpc:
    addr: "127.0.0.1:%d"
  data: {}
`
	eopa, eopaOut := eopaRun(t, policy, data, fmt.Sprintf(config, eopaGRPCPort), eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)
}

func eopaEval(t *testing.T, limit, data string) *exec.Cmd {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	return exec.Command(binary(), strings.Split("eval --instruction-limit "+limit+" --data "+dataPath+" data.xs[_]", " ")...)
}

func eopaRun(t *testing.T, policy, data, config string, httpPort int, extraArgs ...string) (*exec.Cmd, *bytes.Buffer) {
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
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	args = append(args, extraArgs...)
	args = append(args,
		dataPath,
		policyPath,
	)
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

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

func largeJSON() string {
	data := strings.Builder{}
	data.WriteString(`{"xs": [`)
	for i := 0; i <= 1000; i++ {
		if i != 0 {
			data.WriteRune(',')
		}
		data.WriteString(`{"a": 1, "b": 2}`)
	}
	data.WriteString(`]}`)
	return data.String()
}
