package cmd

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/internal/license"
	keygen "github.com/styrainc/enterprise-opa-private/internal/license"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

const brand = "Enterprise OPA"

func addLicenseFlags(c *cobra.Command, licenseParams *keygen.LicenseParams) {
	c.Flags().StringVar(&licenseParams.Key, "license-key", "", "Location of file containing EOPA_LICENSE_KEY")
	c.Flags().StringVar(&licenseParams.Token, "license-token", "", "Location of file containing EOPA_LICENSE_TOKEN")
}

func addInstructionLimitFlag(c *cobra.Command, instrLimit *int64) {
	c.Flags().Int64Var(instrLimit, "instruction-limit", 100_000_000, "set instruction limit for VM")
}

func EnterpriseOPACommand(lic license.Checker) *cobra.Command {
	var instructionLimit int64

	// For all Enterprise OPA commands, the VM is the default target. It can be
	// overridden for `eopa eval` by providing `-t rego`.
	rego_vm.SetDefault(true)

	// These flags are added to `eopa eval` (OPA doesn't have them). They are
	// then passed on to the logger used with keygen for license (de)activation,
	// heartbeating, etc. There is no extra log output from the actual policy
	// eval, and the logger is not made available to that code.
	// It's really only meant for debugging license trouble.
	logLevel := util.NewEnumFlag("info", []string{"debug", "info", "error"})
	logFormat := util.NewEnumFlag("json", []string{"json", "json-pretty"})

	lparams := keygen.NewLicenseParams()

	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Enterprise OPA",

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if instructionLimit > 0 {
				rego_vm.SetLimits(instructionLimit)
			}

			switch cmd.CalledAs() {
			case "eval":
				if lic == nil {
					return
				}

				lvl, _ := getLevel(logLevel.String())
				format := getFormatter(logFormat.String())
				lic.SetFormatter(format)
				lic.SetLevel(lvl)

				// do the license validate and activate asynchronously; so user doesn't have to wait
				go lic.ValidateLicenseOrDie(lparams) // calls os.Exit if license isn't valid
			}
		},
	}

	// add OPA commands to root
	opa := cmd.Command(brand)
	for _, c := range opa.Commands() {
		switch c.Name() {
		case "run":
			addLicenseFlags(c, lparams)
			addInstructionLimitFlag(c, &instructionLimit)
			root.AddCommand(initRun(c, brand, lic, lparams)) // wrap OPA run
		case "eval":
			addLicenseFlags(c, lparams)
			addInstructionLimitFlag(c, &instructionLimit)

			c.Flags().VarP(logLevel, "log-level", "l", "set log level")
			c.Flags().Var(logFormat, "log-format", "set log format") // NOTE(sr): we don't support "text" here

			root.AddCommand(c)
		case "version":
			root.AddCommand(initVersion()) // override version
		default:
			root.AddCommand(c)
		}
	}

	// New Enterprise OPA commands
	root.AddCommand(initBundle())
	root.AddCommand(liaCtl())

	licenseCmd := LicenseCmd(lic, lparams)
	addLicenseFlags(licenseCmd, lparams)
	root.AddCommand(licenseCmd)
	return root
}

// From opa/internal/logging.go
func getLevel(level string) (logging.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return logging.Debug, nil
	case "", "info":
		return logging.Info, nil
	case "warn":
		return logging.Warn, nil
	case "error":
		return logging.Error, nil
	default:
		return logging.Debug, fmt.Errorf("invalid log level: %v", level)
	}
}

func getFormatter(format string) logrus.Formatter {
	return &logrus.JSONFormatter{PrettyPrint: format == "json-pretty"}
}
