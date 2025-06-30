package cmd

import (
	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/v1/hooks"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/sdk"

	"github.com/styrainc/enterprise-opa-private/pkg/ekm"
	eopa_sdk "github.com/styrainc/enterprise-opa-private/pkg/sdk"
)

// Run provides the CLI entrypoint for the `exec` subcommand
func initExec(opa *cobra.Command) *cobra.Command {
	original := opa.RunE
	// Only override Run, so we keep the args and usage texts
	opa.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceErrors = true
		c.SilenceUsage = true

		// Note(philip): Removed license checks here.
		sdk.DefaultOptions = eopa_sdk.DefaultOptions()
		e := ekm.NewEKM()
		e.SetLogger(logging.NewNoOpLogger())
		sdkDefaultOptions := eopa_sdk.DefaultOptions()
		sdkDefaultOptions.Hooks = hooks.New(e)
		sdk.SetDefaultOptions(sdkDefaultOptions)
		enableEOPAOnly()
		return original(c, args)
	}
	return opa
}
