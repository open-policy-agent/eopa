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
func initExec(opa *cobra.Command, lic *license.Checker, lparams *keygen.LicenseParams) *cobra.Command {
	fallback := opa.RunE
	// Only override Run, so we keep the args and usage texts
	opa.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceErrors = true
		c.SilenceUsage = true

		strict, _ := c.Flags().GetBool("no-license-fallback")
		if !strict { // validate license synchronously
			if err := lic.ValidateLicense(c.Context(), lparams); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				fmt.Fprintln(os.Stderr, "Switching to OPA mode")

				c.SilenceErrors = false
				return fallback(c, args)
			}
		} else { // do the license validate and activate asynchronously
			go func() {
				if err := lic.ValidateLicense(c.Context(), lparams); err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(license.ErrorExitCode)
				}
			}()
		}

		sdk.DefaultOptions = eopa_sdk.DefaultOptions()
		e := ekm.NewEKM(lic)
		e.SetLogger(logging.NewNoOpLogger())
		sdk.DefaultOptions.Hooks = hooks.New(e)
		enableEOPAOnly()
		return fallback(c, args)
	}
	return opa
}
