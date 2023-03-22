package cmd

import (
	"os"
	"path"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/pkg/rego_vm"
)

const brand = "Load"

func addLicenseFlags(c *cobra.Command, key *string, token *string) {
	c.Flags().StringVar(key, "license-key", "", "Location of file containing STYRA_LOAD_LICENSE_KEY")
	c.Flags().StringVar(token, "license-token", "", "Location of file containing STYRA_LOAD_LICENSE_TOKEN")
}

func addInstructionLimitFlag(c *cobra.Command, instrLimit *int64) {
	c.Flags().Int64Var(instrLimit, "instruction-limit", 100_000_000, "set instruction limit for VM")
}

func LoadCommand(license *License) *cobra.Command {
	var key string
	var token string
	var instructionLimit int64

	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			switch cmd.CalledAs() {
			case "eval", "run":
				if instructionLimit > 0 {
					rego_vm.SetLimits(instructionLimit)
				}

				if license == nil {
					return
				}
				go func() {
					// do the license validate and activate asynchronously; so user doesn't have to wait
					license.ValidateLicense(key, token, func(code int, err error) { os.Exit(code) })
				}()
			}
		},
	}

	// add OPA commands to root
	opa := cmd.Command(brand)
	for _, c := range opa.Commands() {
		switch c.Name() {
		case "run":
			addLicenseFlags(c, &key, &token)
			addInstructionLimitFlag(c, &instructionLimit)
			root.AddCommand(initRun(c, brand)) // wrap OPA run
		case "eval":
			addLicenseFlags(c, &key, &token)
			addInstructionLimitFlag(c, &instructionLimit)
			root.AddCommand(c)
		case "version":
			root.AddCommand(initVersion()) // override version
		default:
			root.AddCommand(c)
		}
	}

	// New Load commands
	root.AddCommand(initBundle())
	root.AddCommand(liaCtl())

	licenseCmd := LicenseCmd(license, &key, &token)
	addLicenseFlags(licenseCmd, &key, &token)
	root.AddCommand(licenseCmd)
	return root
}
