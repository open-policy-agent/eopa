//go:build e2e

package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	sdk_test "github.com/open-policy-agent/opa/v1/sdk/test"
)

var _exec = []string{"exec", "--log-level", "debug", "--decision", "/test/p", "/dev/null"} // don't need input

func TestExecFallbackSuccess(t *testing.T) {
	policy := `package test
p := true
` // no builtins
	s := sdk_test.MustNewServer(sdk_test.MockBundle("/bundles/bundle.tar.gz", map[string]string{"test.rego": policy}))
	t.Cleanup(s.Stop)

	config := fmt.Sprintf(`services:
  s:
    url: %s
bundles:
  bundle.tar.gz:
    service: s
    resource: bundles/bundle.tar.gz
`, // no plugins
		s.URL())

	eopa, eopaOut := eopaSansEnv(t, "", config, _exec...)
	if err := eopa.Run(); err != nil {
		t.Fatal(err)
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
}

func TestExecFallbackFailPlugins(t *testing.T) {
	policy := `package test
p := true
` // no builtins
	s := sdk_test.MustNewServer(sdk_test.MockBundle("/bundles/bundle.tar.gz", map[string]string{"test.rego": policy}))
	t.Cleanup(s.Stop)

	config := fmt.Sprintf(`services:
  s:
    url: %s
bundles:
  bundle.tar.gz:
    service: s
    resource: bundles/bundle.tar.gz
decision_logs:
  plugin: eopa dl
plugins:
  eopa_dl:
    output:
      type: console
`, s.URL())

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

func TestExecFallbackFailBuiltin(t *testing.T) {
	policy := `package test
p := sql.send({})
`
	s := sdk_test.MustNewServer(sdk_test.MockBundle("/bundles/bundle.tar.gz", map[string]string{"test.rego": policy}))
	t.Cleanup(s.Stop)

	config := fmt.Sprintf(`services:
  s:
    url: %s
bundles:
  bundle.tar.gz:
    service: s
    resource: bundles/bundle.tar.gz
`, // no plugins
		s.URL())

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
