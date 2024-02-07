//go:build e2e

package run

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/styrainc/enterprise-opa-private/e2e/cli/run/testdata"
	"github.com/styrainc/enterprise-opa-private/e2e/utils"
)

func TestRun(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   utils.ExplodeEmbed(t, testdata.FS),
		Setup: utils.IncludeLicenseEnvVars,
		Cmds:  utils.TestscriptExtraFunctions(),
	})
}
