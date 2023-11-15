//go:build e2e

package login

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestConfigFileAndArgs(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata",
	})
}
