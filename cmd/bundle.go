package cmd

import "github.com/spf13/cobra"

func initBundle() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Enterprise OPA Bundle commands",
	}
	cmd.AddCommand(Convert())
	cmd.AddCommand(Dump())
	return cmd
}
