//go:build e2e

package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNoLicenseForEval(t *testing.T) {
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
	ee := err.(*exec.ExitError)
	prefix := `invalid license: no license provided

Sign up for a free trial now`
	if act := string(ee.Stderr); !strings.HasPrefix(act, prefix) {
		t.Errorf("expected output %s, got %s", prefix, act)
	}
}

func TestNoLicenseForQuickEval(t *testing.T) {
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
	ee := err.(*exec.ExitError)
	exp := "invalid license: invalid license: license key is invalid\n"
	if act := string(ee.Stderr); exp != act {
		t.Errorf("expected output %s, got %s", exp, act)
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
