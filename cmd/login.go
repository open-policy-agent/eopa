// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
)

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "login",
		Deprecated: "Command no longer used in EOPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	return cmd
}
