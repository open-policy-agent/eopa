package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/hooks"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/sdk"

	"github.com/styrainc/enterprise-opa-private/internal/license"
	keygen "github.com/styrainc/enterprise-opa-private/internal/license"
	"github.com/styrainc/enterprise-opa-private/pkg/ekm"
	eopa_sdk "github.com/styrainc/enterprise-opa-private/pkg/sdk"
)

// Run provides the CLI entrypoint for the `exec` subcommand
func initExec(opa *cobra.Command, license license.Checker, lparams *keygen.LicenseParams) *cobra.Command {
	fallback := opa.RunE
	// Only override Run, so we keep the args and usage texts
	opa.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceErrors = true
		c.SilenceUsage = true

		strict, _ := c.Flags().GetBool("no-license-fallback")
		license.SetStrict(strict)
		if !strict { // validate license synchronously
			if err := license.ValidateLicense(lparams); err != nil { // TODO(sr): context? timeout?
				fmt.Fprintln(os.Stderr, err.Error())
				fmt.Fprintln(os.Stderr, "Switching to OPA mode")

				c.SilenceErrors = false
				return fallback(c, args)
			}
		}
		sdk.DefaultOptions = eopa_sdk.DefaultOptions()
		// Update EKM so it'll deal with the license checking asynchronously
		e := ekm.NewEKM(license, lparams)
		e.SetLogger(logging.NewNoOpLogger())
		sdk.DefaultOptions.Hooks = hooks.New(e)
		return fallback(c, args)
	}
	return opa
}
