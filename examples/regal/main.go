package main

import (
	"log"
	"os"

	"github.com/styrainc/regal/cmd"

	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func main() {
	log.SetFlags(0)
	rego_vm.SetDefault(true)
	if err := cmd.RootCommand.Execute(); err != nil {
		os.Exit(1)
	}
}
