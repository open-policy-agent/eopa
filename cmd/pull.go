// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// targetDir says where to pull libraries to
const (
	targetDir        = "libraries"      // that's the flag, --libraries, and the config key
	targetDirDefault = ".styra/include" // that's the folder
	apiTokenEnvVar   = "EOPA_STYRA_DAS_TOKEN"
)

// force makes the pull command write to libraries even when it exists
const force = "force"

func pullCmd(_ *viper.Viper, _ []string) *cobra.Command {
	cmd := &cobra.Command{
		Use: "pull",
		Example: `
Download all DAS libraries using settings from .styra.yaml:

    eopa pull

Note: 'eopa pull' will look for .styra.yaml in the current directory,
the repository root, and your home directory. To use a different config
file location, pass --styra-config:

    eopa pull --styra-config ~/.styra-primary.yaml

If the environment varable EOPA_STYRA_DAS_TOKEN is set, 'eopa pull'
will use it as an API token to talk to the configured DAS instance:

    EOPA_STYRA_DAS_TOKEN="..." eopa pull

Write all libraries to to libs/, with debug logging enabled:

    eopa pull --libraries libs --log-level debug

Remove files that aren't expected in the target directory:

    eopa pull --force
`,
		Short:      "Pull libraries from DAS instance",
		Deprecated: "Command no longer used in Enterprise OPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	addDASFlags(cmd)
	cmd.Flags().BoolP(force, "f", false, "ignore if libraries folder exists, overwrite existing content on conflict")
	return cmd
}
