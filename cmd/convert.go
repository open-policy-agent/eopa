package cmd

import (
	"github.com/spf13/cobra"

	"github.com/styrainc/enterprise-opa-private/pkg/convert"
)

func Convert() *cobra.Command {
	return &cobra.Command{
		Use:   "convert",
		Short: "Convert OPA bundle to binary bundle",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			c.SilenceUsage = true
			return convert.BundleFile(args[0], args[1])
		},
	}
}
