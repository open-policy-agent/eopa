package cmd

import (
	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/pkg/rego_vm"
)

func evalWrap(c *cobra.Command, limit *int64) *cobra.Command {
	inner := c.RunE
	c.RunE = func(c *cobra.Command, args []string) error {
		if limit != nil {
			rego_vm.SetLimits(*limit)
		}
		return inner(c, args)
	}
	return c
}
