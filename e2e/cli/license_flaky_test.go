//go:build e2e && flaky

package cli

import (
	"strings"
	"testing"

	"github.com/styrainc/enterprise-opa-private/e2e/retry"
)

func TestNoLicenseForEval(t *testing.T) {
	retry.Run(t, func(r *retry.R) {
		// If we run `eopa eval` without a license from env vars/CLI args, it'll fail
		// and suggest starting a trial. Since the license check happens asynchronously
		// -- to not stall `eopa eval` calls -- bad licenses can go unnoticed with quick
		// evals (see test below).
		eopa := eopaEvalQuery("true")
		eopa.Env = filter(eopa.Env)
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
			prefix := `invalid license: no license provided

Sign up for a free trial now`
			if act := string(ee.Stderr); !strings.HasPrefix(act, prefix) {
				t.Errorf("expected output %s, got %s", prefix, act)
			}
		} else {
			t.Errorf("expected *exec.ExitError, got %T instead", err)
		}
	})
}
