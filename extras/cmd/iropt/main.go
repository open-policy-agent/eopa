package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/open-policy-agent/opa/ir"
	"github.com/spf13/cobra"
	"github.com/styrainc/enterprise-opa-private/pkg/iropt"
)

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

const (
	megabyte = 1073741824
)

func main() {
	var enableOptPassFlags, disableOptPassFlags iropt.OptimizationPassFlags
	var optLevel int64
	var filename string

	rootCmd := &cobra.Command{
		Use:   "iropt",
		Short: "iropt allows optimizing a Rego IR plan, and supports the same CLI options as EOPA.",
		Long:  ``,
		Run: func(*cobra.Command, []string) {
			// Get input Rego file from stdin or a file on disk.
			var fileBytes bytes.Buffer
			if filename == "" {
				r := bufio.NewReaderSize(os.Stdin, megabyte)
				line, isPrefix, err := r.ReadLine()
				for err == nil {
					fileBytes.Write(line)
					if !isPrefix {
						fileBytes.WriteByte('\n')
					}
					line, isPrefix, err = r.ReadLine()
				}
				if err != io.EOF {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else {
				b, err := os.ReadFile(filename)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				fileBytes.Write(b)
			}

			var policy ir.Policy
			if err := json.Unmarshal(fileBytes.Bytes(), &policy); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			var optimizationSchedule []*iropt.IROptPass
			switch optLevel {
			case 0:
				optimizationSchedule = iropt.NewIROptLevel0Schedule(&enableOptPassFlags, &disableOptPassFlags)
			default:
				// Note(philip): Expand the case list as we accrue more optimization levels.
				optimizationSchedule = iropt.NewIROptLevel0Schedule(&enableOptPassFlags, &disableOptPassFlags)
			}

			optimizedPolicy, err := iropt.RunPasses(&policy, optimizationSchedule)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			bs, err := json.Marshal(optimizedPolicy)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			fmt.Println(string(bs))
		},
	}

	rootCmd.PersistentFlags().StringVarP(&filename, "filename", "f", "", "Rego IR JSON blob to read in and optimize. (default: stdin)")
	addOptimizationFlagsAndDescription(rootCmd, &optLevel, &enableOptPassFlags, &disableOptPassFlags)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
