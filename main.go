package main

import (
	"fmt"
	"os"
	"path"

	loadCmd "github.com/StyraInc/load/cmd"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

func main() {
	load := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",
	}
	for _, c := range cmd.RootCommand.Commands() {
		switch c.Name() {
		case "run": // redo
			load.AddCommand(loadCmd.Run(c))
		default:
			load.AddCommand(c)
		}

	}
	if err := load.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}