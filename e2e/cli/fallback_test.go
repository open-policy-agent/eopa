//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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

// server CLI args used in these tests
var server = []string{"run", "--server", "--disable-telemetry", "--log-level", "debug"}

func TestRunServerFallbackSuccess(t *testing.T) {
	config := `` // no plugins
	policy := `package test
p := true
q if input.foo.bar = "baz"` // no builtins called

	serverArgs := append(server, "--addr", fmt.Sprintf(":%d", eopaHTTPPort))
	eopa, eopaOut := eopaSansEnv(t, policy, config, serverArgs...)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}

	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "no license provided") }, time.Second)
	wait.ForLog(t, eopaOut, func(s string) bool {
		return strings.Contains(s, "Sign up for a free trial now by running `eopa license trial`")
	}, time.Second)
	wait.ForLog(t, eopaOut, func(s string) bool {
		return strings.Contains(s, "Switching to OPA mode. Enterprise OPA functionality will be disabled.")
	}, time.Second)

	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	if t.Failed() {
		t.Logf("early output: %s", eopaOut.String())
	}

	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	t.Run("data api", func(t *testing.T) { // Data API
		req, err := http.NewRequest("POST", "http://localhost:"+fmt.Sprintf("%d", eopaHTTPPort)+"/v1/data/test/p?metrics&instrument", nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusOK, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Result  any
			Metrics map[string]any
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := true, output.Result; exp != act {
			t.Fatalf("expected result %v, got %v", exp, act)
		}

		// NOT having this metric, but having the other one indicates that topdown was used for eval
		if _, ok := output.Metrics["counter_regovm_eval_instructions"]; ok {
			t.Fatalf("expected metric counter_regovm_eval_instructions to be missing, found it: %v", output.Metrics)
		}
		if _, ok := output.Metrics["histogram_eval_op_rule_index"]; !ok {
			t.Fatalf("expected metric histogram_eval_op_rule_index, not found: %v", output.Metrics)
		}
	})

	t.Run("compile-api-compat", func(t *testing.T) { // Compile API (compat) works fine
		payload := map[string]any{
			"query": "data.test.q",
		}
		jsonPayload := new(bytes.Buffer)
		if err := json.NewEncoder(jsonPayload).Encode(payload); err != nil {
			t.Fatalf("json encode: %v", err)
		}
		req, err := http.NewRequest("POST", "http://localhost:"+fmt.Sprintf("%d", eopaHTTPPort)+"/v1/compile", jsonPayload)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusOK, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Result struct {
				Queries []any
			}
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := 1, len(output.Result.Queries); exp != act {
			t.Fatalf("expected %v queries, got %v", exp, act)
		}
	})

	t.Run("compile-api-extension", func(t *testing.T) { // Compile API (extensions) -- returns an error
		payload := map[string]any{}
		jsonPayload := new(bytes.Buffer)
		if err := json.NewEncoder(jsonPayload).Encode(payload); err != nil {
			t.Fatalf("json encode: %v", err)
		}
		req, err := http.NewRequest("POST", "http://localhost:"+fmt.Sprintf("%d", eopaHTTPPort)+"/v1/compile/test/q", jsonPayload)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		req.Header.Set("Accept", "application/vnd.styra.sql.mysql+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusNotImplemented, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Code    string
			Message string
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := "license-required", output.Code; exp != act {
			t.Errorf("expected code %v, got %v", exp, act)
		}
		if exp, act := "requested API extension unavailable in fallback mode", output.Message; exp != act {
			t.Errorf("expected message %v, got %v", exp, act)
		}
	})
}

func TestRunServerFallbackFailPlugins(t *testing.T) {
	config := `
decision_logs:
  plugin: eopa dl
plugins:
  eopa_dl:
    output:
      type: console
`
	policy := `package test
p := true` // no builtins called

	serverArgs := append(server, "--addr", fmt.Sprintf(":%d", eopaHTTPPort))
	eopa, _ := eopaSansEnv(t, policy, config, serverArgs...)
	out, err := eopa.Output()
	if err == nil {
		t.Fatal("expected error")
	}
	if ee := (&exec.ExitError{}); errors.As(err, &ee) {
		if exp, act := "", string(ee.Stderr); exp != act {
			t.Errorf("expected stderr = %q, got %q", exp, act)
		}
	}
	if exp, act := `error: config error: plugin "eopa_dl" not registered`, strings.TrimSpace(string(out)); exp != act {
		t.Errorf("expected stdout = %q, got %q", exp, act)
	}
}

func TestRunServerFallbackFailBuiltin(t *testing.T) {
	config := `` // no plugins
	policy := `package test
p := sql.send({})
`

	serverArgs := append(server, "--addr", fmt.Sprintf(":%d", eopaHTTPPort))
	eopa, _ := eopaSansEnv(t, policy, config, serverArgs...)
	out, err := eopa.Output()
	if err == nil {
		t.Fatal("expected error")
	}
	if ee := (&exec.ExitError{}); errors.As(err, &ee) {
		if exp, act := "", string(ee.Stderr); exp != act {
			t.Errorf("expected stderr = %q, got %q", exp, act)
		}
	}
	// NOTE(sr): using HasSuffix because the error has the temp file path in it
	if exp, act := `eval.rego:2: rego_type_error: undefined function sql.send`, strings.TrimSpace(string(out)); !strings.HasSuffix(act, exp) {
		t.Errorf("expected stdout = %q, got %q", exp, act)
	}
}

func eopaSansEnv(t *testing.T, policy, config string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	return eopaFilterEnv(t, policy, config, filter, args...)
}

func eopaCmd(t *testing.T, policy, config string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	return eopaFilterEnv(t, policy, config, nil, args...)
}

func eopaFilterEnv(t *testing.T, policy, config string, f func([]string) []string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	if policy != "" {
		policyPath := filepath.Join(dir, "eval.rego")
		if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		args = append(args, policyPath)
	}
	eopa := exec.Command(binary(), args...)
	if f != nil {
		eopa.Env = f(eopa.Env)
	}
	eopa.Stderr = &buf

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		_ = eopa.Process.Signal(os.Interrupt)
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa output:\n%s", buf.String())
		}
	})

	return eopa, &buf
}
