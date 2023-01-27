package main

import (
	"errors"
	"os"
	"path"

	loadCmd "github.com/styrainc/load/cmd"
	_ "github.com/styrainc/load/pkg/rego_vm"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

func main() {
	// run all deferred functions before os.Exit
	var exit int
	defer func() {
		if exit != 0 {
			os.Exit(exit)
		}
	}() // orderly shutdown, run all defer routines

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
		var e *cmd.ExitError
		if errors.As(err, &e) {
			exit = e.Exit
		} else {
			exit = 1
		}
		return
	}
}
