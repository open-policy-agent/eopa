//go:build e2e

package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/styrainc/enterprise-opa-private/e2e/retry"
)

func TestNoLicenseForQuickEval(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		// Bad license keys can go unnoticed for quick evals. See comment
		// above.
		eopa := eopaEvalQuery("true")
		eopa.Env = append(filter(eopa.Env), "EOPA_LICENSE_KEY=invalid")
		stdout, err := eopa.Output()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if exp, act := "true\n", string(stdout); exp != act {
			t.Errorf("expected stdout %s, got %s", exp, act)
		}
	})
}

func TestNoLicenseForLongEval(t *testing.T) {
	// Bad license keys can go unnoticed for quick evals. See comment
	// above.
	eopa := eopaEvalQuery("_ = numbers.range(1, 10e10)")
	eopa.Env = append(filter(eopa.Env), "EOPA_LICENSE_KEY=invalid")
	stdout, err := eopa.Output()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(stdout) > 0 {
		t.Errorf("expected empty stdout, got %s", stdout)
	}
	if e, ok := err.(interface{ ExitCode() int }); ok {
		if exp, act := 3, e.ExitCode(); exp != act {
			t.Errorf("expected exit code %d, got %d", exp, act)
		}
	}
	if ee, ok := err.(*exec.ExitError); ok {
		exp := "invalid license: invalid license: license key is invalid\n"
		if act := string(ee.Stderr); exp != act {
			t.Errorf("expected output %s, got %s", exp, act)
		}
	} else {
		t.Errorf("expected *exec.ExitError, got %T instead", err)
	}
}

func eopaEvalQuery(query string) *exec.Cmd {
	return exec.Command(binary(), "eval", "-fpretty", query)
}

func filter(in []string) []string {
	out := []string{}
	for i := range in {
		if !strings.HasPrefix(in[i], "EOPA") {
			out = append(out, in[i])
		}
	}
	return out
}
