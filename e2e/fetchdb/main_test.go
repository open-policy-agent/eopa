//go:build e2e_fetchdb

package fetchdb

import (
	"bufio"
	"bytes"
	"testing"
	"os"
	"os/exec"

	"golang.org/x/exp/slices"
)

var exceptions = []string{
	// These three are instances of https://github.com/StyraInc/load-private/issues/117
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_item_error",
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_systemError",
	"data.library.v1.kubernetes.admission.workload.test_v1.test_validate_image_signature_with_cosign_HTTP_non_200_response",
}

func TestAllRegoTestsInFetchdb(t *testing.T) {
	fails := []string{}
	cmd := exec.Command("make", "load-test")
	cmd.Dir = os.Getenv("FETCHDB_DIRECTORY")
	output, err := cmd.Output()
	if err != nil {
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