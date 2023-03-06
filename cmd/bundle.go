package cmd

import "github.com/spf13/cobra"

func Bundle() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Styra Load Bundle commands",
	}
	cmd.AddCommand(Convert())
	cmd.AddCommand(Dump())
	return cmd
}
