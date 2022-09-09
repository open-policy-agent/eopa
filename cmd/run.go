package cmd

import (
	"github.com/spf13/cobra"
)

// Run provides the CLI entrypoint for the `run` subcommand
func Run(opa *cobra.Command) *cobra.Command {
	// Only override Run, so we keep the args and usage texts
	opa.Run = func(*cobra.Command, []string) {
		panic("todo")
	}
	return opa
}
