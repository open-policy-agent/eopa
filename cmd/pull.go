// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
)

// force makes the pull command write to libraries even when it exists
const force = "force"

func pullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "pull",
		Deprecated: "Command no longer used in EOPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	return cmd
}
