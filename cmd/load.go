package cmd

import "github.com/spf13/cobra"

func Load() *cobra.Command {
	return &cobra.Command{
		Use:   "load",
		Short: "Styra Load specific commands",
	}
}
