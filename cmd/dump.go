package cmd

import (
	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/pkg/convert"
)

func Dump() *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dump binary bundle data",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			c.SilenceUsage = true
			return convert.DumpData(args[0])
		},
	}
}
