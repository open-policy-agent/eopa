package cmd

import (
	"github.com/spf13/cobra"

	"github.com/StyraInc/load/pkg/convert"
)

func Dump() *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dump binary bundle data",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return convert.DumpData(args[0])
		},
	}
}
