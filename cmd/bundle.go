package cmd

import "github.com/spf13/cobra"

func Bundle() *cobra.Command {
	return &cobra.Command{
		Use:   "bundle",
		Short: "Styra Load Bundle commands",
	}
}
