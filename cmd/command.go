// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	opa_cmd "github.com/open-policy-agent/opa/cmd"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/util"
	opa_version "github.com/open-policy-agent/opa/v1/version"

	internal_logging "github.com/open-policy-agent/eopa/internal/logging"
	"github.com/open-policy-agent/eopa/internal/version"
	"github.com/open-policy-agent/eopa/pkg/builtins"
	"github.com/open-policy-agent/eopa/pkg/iropt"
	"github.com/open-policy-agent/eopa/pkg/plugins/bundle"
	"github.com/open-policy-agent/eopa/pkg/rego_vm"
)

const brand = "Enterprise OPA"

// Key for matching up --data with the config. The semantics are a little weird
// because some subcommand receive the extra data paths as extra CLI arguments,
// while others actually define a --data flag.
const dataKey = "data"

func addLicenseFlags(c *cobra.Command) {
	c.Flags().String("license-key", "", "Location of file containing EOPA_LICENSE_KEY")
	c.Flags().String("license-token", "", "Location of file containing EOPA_LICENSE_TOKEN")

	c.Flags().MarkDeprecated("license-key", "deprecated flag")
	c.Flags().MarkDeprecated("license-token", "deprecated flag")
}

func addLicenseFallbackFlags(c *cobra.Command) {
	c.Flags().Bool("no-license-fallback", false, "Don't fall back to OPA-mode when no license provided.")

	c.Flags().MarkDeprecated("no-license-fallback", "deprecated flag")
}

func addDiscoveryLicenseFlags(c *cobra.Command) {
	c.Flags().Bool("no-discovery-license-check", false, "Disable discovery-based licensing check.")
	c.Flags().Int("license-discovery-timeout", 30, "Timeout (in seconds) for discovery-based licensing check.")

	c.Flags().MarkDeprecated("no-discovery-license-check", "deprecated flag")
	c.Flags().MarkDeprecated("license-discovery-timeout", "deprecated flag")
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

func EnterpriseOPACommand() *cobra.Command {
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

	root := &cobra.Command{
		Use:   "eopa",
		Short: "Enterprise OPA",

		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
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
				logger, err := getLogger(logLevel.String(), logFormat.String(), "")
				if err != nil {
					return err
				}

				selectedPath, _ := cmd.Flags().GetString(styraConfig)
				if err := readConfig(selectedPath, cfg, paths, logger); err != nil {
					return err
				}

				// Note that we don't use cfg.BindPFlag() here,
				// it seems that when we do this, it can cause
				// merge errors while loading data, which I
				// suspect is due to trying to load the same
				// data file more than once. Viper appears to
				// have some issue with our repeated string
				// flag type. Instead, we directly read the
				// values out of the config as a slice and
				// insert them into the flag here.
				//
				// -- CAD 2023-12-12

				for _, s := range cfg.GetStringSlice(dataKey) {
					// Note that in the OPA implementation
					// of repeated string arguments, the
					// .Set() function actually appends
					// to the slice of values, it does not
					// replace the existing contents.
					//
					// -- CAD 2023-12-07
					cmd.Flags().Lookup(dataKey).Value.Set(s)
				}

				if p, _ := cmd.Flags().GetBool("profile"); p {
					t := cmd.Flag("target")
					if !t.Changed || t.Value.String() != "rego" { // not explicitly requested rego already
						logger.Warn("profiling is not supported in Enterprise OPA, switching to OPA's evaluation mode")
						t.Value.Set("rego")
					}
				}

			case "test":
				logger, err := getLogger(logLevel.String(), logFormat.String(), "")
				if err != nil {
					return err
				}

				selectedPath, _ := cmd.Flags().GetString(styraConfig)
				if err := readConfig(selectedPath, cfg, paths, logger); err != nil {
					return err
				}

				// We have to monkey-patch the `opa test`
				// command's RunE() function, because there is
				// no other way to mutate the arguments passed
				// to it. The command's args are already read
				// in Cobra _before_ any pre-run functions are
				// called, so attempting to use cmd.SetArgs()
				// or mutating os.Args won't work at this
				// stage. The only way we can properly interdict
				// the arguments is to insert them into the
				// RunE callback itself. See the
				// `*Command.execute()` implementation in
				// Cobra:
				//
				// https://github.com/spf13/cobra/blob/3d8ac432bdad89db04ab0890754b2444d7b4e1cf/command.go#L874
				//
				// When I wrote this, it looked like the test
				// command in the OPA source tree was defining
				// .Run, not .RunE. Yet, the .Run pointer is
				// nil while RunE was non-nil. Maybe we patched
				// it in the fork, or there are other
				// shenanigans afoot.
				//
				// -- CAD 2023-12-07
				extraDataArgs := cfg.GetStringSlice(dataKey)

				bundle, err := cmd.Flags().GetBool("bundle")
				if err != nil {
					return err
				}
				if (len(extraDataArgs) > 0) && bundle {
					logger.Warn("--bundle is asserted and %d additional data paths picked up from config file, this is likely to cause an overlapping roots error", len(extraDataArgs))
				}

				oldRunE := cmd.RunE
				cmd.RunE = func(cmd *cobra.Command, args []string) error {
					return oldRunE(cmd, append(args, extraDataArgs...))
				}

			case "run":
				logger, err := getLogger(logLevel.String(), logFormat.String(), "")
				if err != nil {
					return err
				}

				selectedPath, _ := cmd.Flags().GetString(styraConfig)
				if err := readConfig(selectedPath, cfg, paths, logger); err != nil {
					return err
				}

				// See corresponding implementation for `eopa
				// test` above.
				extraDataArgs := cfg.GetStringSlice(dataKey)

				bundle, err := cmd.Flags().GetBool("bundle")
				if err != nil {
					return err
				}
				if (len(extraDataArgs) > 0) && bundle {
					logger.Warn("--bundle is asserted and %d additional data paths picked up from config file, this is likely to cause an overlapping roots error", len(extraDataArgs))
				}

				oldRunE := cmd.RunE
				cmd.RunE = func(cmd *cobra.Command, args []string) error {
					return oldRunE(cmd, append(args, extraDataArgs...))
				}
			}
			return nil
		},
	}

	// add OPA commands to root
	dummyRoot := &cobra.Command{Use: "eopa"}

	opa := opa_cmd.Command(dummyRoot, brand)
	for _, c := range opa.Commands() {
		switch c.Name() {
		case "run":
			addLicenseFlags(c)
			addLicenseFallbackFlags(c)
			addDiscoveryLicenseFlags(c)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)
			root.AddCommand(initRun(c, brand)) // wrap OPA run
		case "eval":
			addLicenseFlags(c)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)

			c.Flags().VarP(logLevel, "log-level", "l", "set log level")
			c.Flags().Var(logFormat, "log-format", "set log format") // NOTE(sr): we don't support "text" here

			root.AddCommand(setDefaults(c))

		case "test":
			addLicenseFlags(c)

			c.Flags().VarP(logLevel, "log-level", "l", "set log level")
			c.Flags().Var(logFormat, "log-format", "set log format") // NOTE(sr): we don't support "text" here

			// Sub-commands:
			c.AddCommand(testBootstrapCmd(cfg, paths))
			c.AddCommand(testNewCmd(cfg, paths))

			root.AddCommand(setDefaults(c))

		case "exec":
			addLicenseFlags(c)
			addLicenseFallbackFlags(c)
			addInstructionLimitFlag(c, &instructionLimit)
			addOptimizationFlagsAndDescription(c, &optLevel, &enableOptPassFlags, &disableOptPassFlags)
			root.AddCommand(initExec(c)) // wrap OPA exec
		case "version":
			root.AddCommand(initVersion()) // override version
		default:
			root.AddCommand(setDefaults(c))
		}
	}

	// New Enterprise OPA commands
	root.AddCommand(initBundle())
	root.AddCommand(liaCtl())
	root.AddCommand(regal())

	root.AddCommand(loginCmd(cfg, paths))
	root.AddCommand(pullCmd(cfg, paths))

	licenseCmd := LicenseCmd()
	addLicenseFlags(licenseCmd)
	root.AddCommand(licenseCmd)
	return root
}

func enableEOPAOnly() {
	rego_vm.SetDefault(true)
	bundle.RegisterActivator()
	builtins.Init()
	opa_cmd.UserAgent(version.UserAgent())
	opa_version.Version = version.Version
}

func setDefaults(c *cobra.Command) *cobra.Command {
	switch {
	case c.RunE != nil:
		prev := c.RunE
		c.RunE = func(c *cobra.Command, args []string) error {
			enableEOPAOnly()
			return extraHints(c, prev(c, args))
		}
	case c.Run != nil:
		prev := c.Run
		c.Run = func(c *cobra.Command, args []string) {
			enableEOPAOnly()
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
	if s, err := os.Stat(filepath.Join(c, ".git")); err == nil && s.IsDir() {
		return c
	}
	ndir := filepath.Dir(c)
	if len(ndir) == len(c) {
		return ""
	}
	return traverseUp(ndir)
}

func getLogger(logLevel string, format, timestampFormat string) (logging.Logger, error) {
	logger := logging.New()
	level, err := internal_logging.GetLevel(logLevel)
	if err != nil {
		return nil, err
	}
	logger.SetLevel(logging.Level(level))
	logger.SetFormatter(internal_logging.GetFormatter(format, timestampFormat))

	return logger, nil
}
