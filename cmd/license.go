// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/spf13/cobra"
)

func LicenseCmd() *cobra.Command {
	c := &cobra.Command{
		Use:        "license",
		Short:      "License status",
		Long:       "View details about an Enterprise OPA license key or token.",
		Deprecated: "Command no longer used in Enterprise OPA",
		RunE: func(c *cobra.Command, _ []string) error {
			c.SilenceErrors = true
			c.SilenceUsage = true
			return nil
		},
	}
	c.AddCommand(TrialCmd())

	return c
}

func TrialCmd() *cobra.Command {
	c := &cobra.Command{
		Use:          "trial",
		Short:        "Create a new Enterprise OPA trial license.",
		Long:         "Gather all of the data needed to create a new Enterprise OPA trial license and create one. Any information not provided via flags is collected interactively. Upon success, the new trial license key is printed to stdout.",
		Deprecated:   "Command no longer used in Enterprise OPA",
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	flags := c.Flags()
	flags.String("first-name", "", "first name to attach to the trial license")
	flags.String("last-name", "", "last name to attach to the trial license")
	flags.String("email", "", "a work email address to attach to the trial license")
	flags.String("company", "", "the company name to attach to the trial license")
	flags.String("country", "", "the country to attach to the trial license")
	flags.Bool("key-only", false, "on success, print only the license key to stdout")

	return c
}
