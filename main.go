package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/cmd"

	_ "github.com/styrainc/enterprise-opa-private/capabilities"
	eopaCmd "github.com/styrainc/enterprise-opa-private/cmd"
	internal "github.com/styrainc/enterprise-opa-private/internal/cmd"
	"github.com/styrainc/enterprise-opa-private/internal/license"
	"github.com/styrainc/enterprise-opa-private/pkg/library"
	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func init() {
	ast.UpdateCapabilities = internal.EnterpriseOPAExtensions
}

func main() {
	// run all deferred functions before os.Exit
	var exit int
	defer func() {
		if exit != 0 {
			os.Exit(exit)
		}
	}() // orderly shutdown, run all defer routines

	lic := license.NewChecker()
	root := eopaCmd.EnterpriseOPACommand(lic)

	// setup default modules
	if err := library.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exit = 2
	}

	// do release in a defer function; works with panics
	defer lic.ReleaseLicense()

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

// Capabilities + built-in metadata file generation:
//go:generate go run internal/cmd/gencapabilities/main.go capabilities.json
