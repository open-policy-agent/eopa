package cmd

import (
	"os"
	"path"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/internal/license"
	keygen "github.com/styrainc/enterprise-opa-private/internal/license"
	internal_logging "github.com/styrainc/enterprise-opa-private/internal/logging"
	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/iropt"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

const brand = "Enterprise OPA"

func addLicenseFlags(c *cobra.Command, licenseParams *keygen.LicenseParams) {
	c.Flags().StringVar(&licenseParams.Key, "license-key", "", "Location of file containing EOPA_LICENSE_KEY")
	c.Flags().StringVar(&licenseParams.Token, "license-token", "", "Location of file containing EOPA_LICENSE_TOKEN")
}

func addLicenseFallbackFlags(c *cobra.Command) {
	c.Flags().Bool("no-license-fallback", false, "Don't fall back to OPA-mode when no license provided.")
}

func addInstructionLimitFlag(c *cobra.Command, instrLimit *int64) {
	c.Flags().Int64Var(instrLimit, "instruction-limit", 100_000_000, "set instruction limit for VM")
}

func addOptimizationFlagsAndDescription(c *cobra.Command, optLevel *int64, optEnableFlags, optDisableFlags *iropt.OptimizationPassFlags) {
	flags2Fields := optEnableFlags.GetFlagToFieldsMapping()
	enableFieldPtrs := optEnableFlags.GetFieldPtrMapping()
	disableFieldPtrs := optDisableFlags.GetFieldPtrMapping()

	// Add explicit optimization pass enable flags.
	for flag, fieldName := range flags2Fields {
		// Add pass enable flag.
		c.Flags().BoolVar(enableFieldPtrs[fieldName], "of"+flag, false, "")
		c.Flag("of" + flag).Hidden = true // Hide all of these flags by default.
		// Add pass disable flag.
		c.Flags().BoolVar(disableFieldPtrs[fieldName], "ofno"+flag, false, "")
		c.Flag("ofno" + flag).Hidden = true
		// Mark both flags as mutually exclusive.
		c.MarkFlagsMutuallyExclusive("of"+flag, "ofno"+flag)
	}

	// Add -O# flags
	// HACK(philip): We have to do this safety check, because the `eval` command already sets a -O flag.
	if c.Flags().Lookup("O") == nil && c.Flags().Lookup("optimize") == nil {
		c.Flags().Int64VarP(optLevel, "optimize", "O", 0, "set optimization level")
	}
	// Add extra text to the long command description.
	c.Long = c.Long + `
Optimization Flags
------------------

The -O flag controls the optimization level. By default, only a limited selection of the
safest optimizations are enabled at -O=0, with progressively more aggressive optimizations
enabled at successively higher -O levels.

Nearly all optimizations can be controlled directly with enable/disable flags.
The pattern for these flags mimics that of well-known compilers, with -of and -ofno
prefixes controlling enabling and disabling of specific passes, respectively.

The following flags control specific optimizations:

  -oflicm/-ofno-licm
	Controls the Loop-Invariant Code Motion (LICM) pass. LICM is used to automatically
	pull loop-independent code out of loops, dramatically improving performance for most
	iteration-heavy policies. (Enabled by default at -O=0)
`
}

func EnterpriseOPACommand(lic license.Checker) *cobra.Command {
	var instructionLimit int64
	var optLevel int64
	var enableOptPassFlags, disableOptPassFlags iropt.OptimizationPassFlags

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

			// Note(philip): Ensure the global var responsible for the optimization schedule is set correctly.
			var optimizationSchedule []*iropt.IROptPass
			switch optLevel {
			case 0:
				optimizationSchedule = iropt.NewIROptLevel0Schedule(&enableOptPassFlags, &disableOptPassFlags)
			default:
				// Note(philip): Expand the case list as we accrue more optimization levels.
				optimizationSchedule = iropt.NewIROptLevel0Schedule(&enableOptPassFlags, &disableOptPassFlags)
			}
			iropt.RegoVMIROptimizationPassSchedule = optimizationSchedule

			switch cmd.CalledAs() {
			case "eval":
				if lic == nil {
					return
				}

				lvl, _ := internal_logging.GetLevel(logLevel.String())
				format := internal_logging.GetFormatter(logFormat.String(), "")
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
			addLicenseFallbackFlags(c)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)
			root.AddCommand(initRun(c, brand, lic, lparams)) // wrap OPA run
		case "eval":
			addLicenseFlags(c, lparams)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)

			c.Flags().VarP(logLevel, "log-level", "l", "set log level")
			c.Flags().Var(logFormat, "log-format", "set log format") // NOTE(sr): we don't support "text" here

			root.AddCommand(setDefaults(c))
		case "exec":
			addLicenseFlags(c, lparams)
			addLicenseFallbackFlags(c)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)
			root.AddCommand(initExec(c, lic, lparams)) // wrap OPA exec
		case "version":
			root.AddCommand(initVersion()) // override version
		default:
			root.AddCommand(setDefaults(c))
		}
	}

	// New Enterprise OPA commands
	root.AddCommand(initBundle())
	root.AddCommand(liaCtl())

	// NOTE(sr): viper supports a bunch of config file formats, but let's decide
	//           which formats we'd like to support, not just take them all as-is.
	viper.SupportedExts = []string{"yaml"}

	// NOTE(sr): for config file debugging, use this
	// cfg := viper.NewWithOptions(viper.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	cfg := viper.New()
	cfg.SetConfigName(".styra")
	cfg.SetConfigType("yaml")
	paths := []string{"."}
	if p := repoRootPath(); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "$HOME")
	root.AddCommand(loginCmd(cfg, paths))
	root.AddCommand(syncCmd(cfg, paths))

	licenseCmd := LicenseCmd(lic, lparams)
	addLicenseFlags(licenseCmd, lparams)
	root.AddCommand(licenseCmd)
	return root
}

func setDefaults(c *cobra.Command) *cobra.Command {
	init := func() {
		rego_vm.SetDefault(true)
		builtins.Init()
	}
	switch {
	case c.RunE != nil:
		prev := c.RunE
		c.RunE = func(c *cobra.Command, args []string) error {
			init()
			return prev(c, args)
		}
	case c.Run != nil:
		prev := c.Run
		c.Run = func(c *cobra.Command, args []string) {
			init()
			prev(c, args)
		}
	}
	return c
}

// repoRootPath traverses from the current working directory upwards, and
// returns the first directory it finds that also contains a `.git` directory.
func repoRootPath() string {
	c, err := os.Getwd()
	if err != nil {
		return ""
	}
	return traverseUp(c)
}

func traverseUp(c string) string {
	c = filepath.Clean(c)
	if filepath.ToSlash(c) == "/" {
		return ""
	}
	if s, err := os.Stat(filepath.Join(c, ".git")); err == nil && s.IsDir() {
		return c
	}
	return traverseUp(c + "/..")
}
