package main

import (
	"errors"
	"log"
	"os"

	"github.com/styrainc/regal/cmd"

	"github.com/open-policy-agent/eopa/pkg/rego_vm"
)

func main() {
	log.SetFlags(0)
	rego_vm.SetDefault(true)
	if err := cmd.RootCommand.Execute(); err != nil {
		code := 1
		if e := (cmd.ExitError{}); errors.As(err, &e) {
			code = e.Code()
		}

		os.Exit(code)
	}
}
