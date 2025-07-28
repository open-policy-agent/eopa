//go:build e2e_fetchdb

package fetchdb

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/exp/slices"
)

var exceptions = []string{
	// These three are instances of https://github.com/open-policy-agent/eopa/issues/117
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_item_error",
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_systemError",
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_HTTP_non_200_response",
	"data.TYPELIB.library.transform.state.test_v1.test_state_to_plan",
}

func TestAllRegoTestsInFetchdb(t *testing.T) {
	fails := []string{}
	cmd := exec.Command("make", "load-test")
	cmd.Dir = os.Getenv("FETCHDB_DIRECTORY")
	output, err := cmd.Output()
	if err != nil {
		if e := (&exec.ExitError{}); errors.As(err, &e) {
			t.Log(string(e.Stderr))
		}
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Bytes()
		if i := bytes.Index(line, []byte(": FAIL")); i > 0 {
			fails = append(fails, string(line[:i]))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	for _, f := range fails {
		if !slices.Contains(exceptions, f) {
			t.Errorf("unexpected failure: %s", f)
		}
	}
}
