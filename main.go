// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/cmd"

	_ "github.com/open-policy-agent/eopa/capabilities"
	eopaCmd "github.com/open-policy-agent/eopa/cmd"
	"github.com/open-policy-agent/eopa/pkg/library"
	_ "github.com/open-policy-agent/eopa/pkg/rego_vm"
)

func main() {
	// run all deferred functions before os.Exit
	var exit int
	defer func() {
		if exit != 0 {
			os.Exit(exit)
		}
	}() // orderly shutdown, run all defer routines

	root := eopaCmd.EOPACommand()

	// setup default modules
	if err := library.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exit = 2
	}

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
