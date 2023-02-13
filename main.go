package main

import (
	"errors"
	"os"

	"github.com/open-policy-agent/opa/cmd"
	loadCmd "github.com/styrainc/load-private/cmd"
	_ "github.com/styrainc/load-private/pkg/rego_vm"
)

func main() {
	// run all deferred functions before os.Exit
	var exit int
	defer func() {
		if exit != 0 {
			os.Exit(exit)
		}
	}() // orderly shutdown, run all defer routines

	license := loadCmd.NewLicense()
	root := loadCmd.LoadCommand(license)

	defer func() {
		// do release in a defer function; works with panics
		license.ReleaseLicense()
	}()

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
