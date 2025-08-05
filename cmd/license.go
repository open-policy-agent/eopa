// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
)

func LicenseCmd() *cobra.Command {
	c := &cobra.Command{
		Use:        "license",
		Deprecated: "Command no longer used in EOPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}
	c.AddCommand(TrialCmd())

	return c
}

func TrialCmd() *cobra.Command {
	c := &cobra.Command{
		Use:        "trial",
		Deprecated: "Command no longer used in EOPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	return c
}
