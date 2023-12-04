//go:build e2e

package login

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/styrainc/enterprise-opa-private/e2e/cli/version/testdata"
	"github.com/styrainc/enterprise-opa-private/e2e/utils"
)

func TestVersion(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: utils.ExplodeEmbed(t, testdata.FS),
	})
}
