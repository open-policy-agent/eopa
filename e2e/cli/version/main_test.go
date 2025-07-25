//go:build e2e

package login

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/open-policy-agent/eopa/e2e/cli/version/testdata"
	"github.com/open-policy-agent/eopa/e2e/utils"
)

func TestVersion(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: utils.ExplodeEmbed(t, testdata.FS),
	})
}
