//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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
var server = []string{"run", "--server", "--addr", "localhost:38181", "--disable-telemetry", "--log-level", "debug"}

func TestRunServerFallbackSuccess(t *testing.T) {
	config := `` // no plugins
	policy := `package test
p := true` // no builtins called

	eopa, eopaOut := eopaSansEnv(t, policy, config, server...)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}

	// NOTE(sr): wait.ForLog will skip anything non-JSON from the output, so we cannot see the warning.
	// So we'll first check the output and then wait for successful (OPA) server init.
	for eopaOut.Len() < 100 {
		time.Sleep(10 * time.Millisecond)
	}
	for _, exp := range []string{
		"no license provided",
		"Sign up for a free trial now by running `eopa license trial`",
		"Switching to OPA mode",
	} {
		if !strings.Contains(eopaOut.String(), exp) {
			t.Errorf("expected %q in output, haven't found it", exp)
		}
	}
	if t.Failed() {
		t.Logf("early output: %s", eopaOut.String())
	}

	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	req, err := http.NewRequest("POST", "http://localhost:38181/v1/data/test/p?metrics&instrument", nil)
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

	eopa, _ := eopaSansEnv(t, policy, config, server...)
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

	eopa, _ := eopaSansEnv(t, policy, config, server...)
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
