//go:build e2e

package bootstrap

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/styrainc/enterprise-opa-private/e2e/cli/test/bootstrap/testdata"
	"github.com/styrainc/enterprise-opa-private/e2e/utils"
)

func TestTests(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   utils.ExplodeEmbed(t, testdata.FS),
		Setup: utils.IncludeLicenseEnvVars,
	})
}
