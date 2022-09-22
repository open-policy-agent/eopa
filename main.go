package main

import (
	"os"
	"path"

	loadCmd "github.com/StyraInc/load/cmd"
	_ "github.com/StyraInc/load/pkg/rego_vm"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",
	}
	for _, c := range cmd.RootCommand.Commands() {
		switch c.Name() {
		case "run":
			root.AddCommand(loadCmd.Run(c))
		default:
			root.AddCommand(c)
		}
	}
	load := loadCmd.Load()
	load.AddCommand(loadCmd.Convert())
	load.AddCommand(loadCmd.Dump())

	root.AddCommand(load)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
