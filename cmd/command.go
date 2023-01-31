package cmd

import (
	"os"
	"path"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

func LoadCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",
	}
	for _, c := range cmd.RootCommand.Commands() {
		switch c.Name() {
		case "run":
			root.AddCommand(Run(c))
		default:
			root.AddCommand(c)
		}
	}
	load := Load()
	load.AddCommand(Convert())
	load.AddCommand(Dump())

	root.AddCommand(load)

	return root
}
