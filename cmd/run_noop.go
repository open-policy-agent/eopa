//go:build !use_opa_fork

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/styrainc/enterprise-opa-private/internal/license"
)

func initRun(*cobra.Command, string, license.Checker, *license.LicenseParams) *cobra.Command {
	return nil
}
