// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/open-policy-agent/opa/v1/logging"

	"github.com/open-policy-agent/eopa/pkg/test_bootstrap"
)

// Sub-command for automated test stub/mock generation.
func testBootstrapCmd(config *viper.Viper, paths []string) *cobra.Command {
	var dataPaths, ignoreNames []string
	var logger logging.Logger
	var err error

	cmd := &cobra.Command{
		Use: "bootstrap [flags] entrypoint [...entrypoint]",
		Example: `
Automatically generate tests for a Rego bundle, based on the policy code
and top-level rules:

    eopa test bootstrap -d policy/ my/policy/entrypoint

Note: If using a standard Styra DAS bundle structure, the policy entrypoint
should always be 'main/main':

    eopa test bootstrap -d das-policy/ main/main

Note: 'eopa test bootstrap' will look for .styra.yaml in the current
directory, the repository root, and your home directory. To use a different
config file location, pass --styra-config:

    eopa test bootstrap \
	  --styra-config ~/.styra-primary.yaml \
	  -d das-policy/ \
	  main/main

This command will attempt to generate test mocks automatically to exercise
each top-level rule specified. For full test coverage, additional tests
and test cases may be required!
`,
		Short: "Generate Rego test mocks automatically from Rego files or bundles",
		// Note(philip): In Cobra, the Args validation checks are run *before*
		// the PreRunE or RunE functions, so if we want a reliable error message
		// for the name collision case with `eopa test bootstrap`, then we have
		// to do the check here.
		Args: func(_ *cobra.Command, args []string) error {
			// Pre-flight check to ensure we didn't have a name collision with `eopa test`!
			if _, err := os.Stat("bootstrap"); !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("Name collision between 'eopa test' and 'eopa test bootstrap' commands detected. No tests will be run or generated.") // nolint:staticcheck
			}

			if len(args) == 0 {
				return fmt.Errorf("need at least 1 entrypoint")
			}
			return nil
		},
		PreRunE: func(c *cobra.Command, _ []string) error {
			bindDASFlags(config, c)
			c.SilenceUsage = true

			lvl, _ := c.Flags().GetString(logLevel)
			format, _ := c.Flags().GetString("log-format")
			logger, err = getLogger(lvl, format, "")
			if err != nil {
				return err
			}

			path, _ := c.Flags().GetString(styraConfig)
			return readConfig(path, config, paths, logger)
		},
		RunE: func(c *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			// TODO(philip): Add DAS/Styra config auto-detection or includes here.

			entrypoints := args
			forceOverwrite, _ := c.Flags().GetBool(force)

			return test_bootstrap.StartBootstrap(ctx,
				test_bootstrap.Entrypoints(entrypoints),
				test_bootstrap.Logger(logger),
				test_bootstrap.DataPaths(dataPaths),
				test_bootstrap.Ignores(ignoreNames),
				test_bootstrap.Force(forceOverwrite),
			)
		},
	}

	addDASFlags(cmd) // TODO(philip): Do we want "bindDASFlags" here?
	cmd.Flags().StringSliceVarP(&ignoreNames, "ignore", "", []string{}, "set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)")
	cmd.Flags().StringSliceVarP(&dataPaths, "data", "d", []string{}, "set policy or data file(s). Recursively traverses bundle folders. This flag can be repeated.")
	cmd.Flags().BoolP("force", "f", false, "ignore if test files already exist, overwrite existing content on conflict")
	return cmd
}
