// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"os"

	"github.com/open-policy-agent/opa/v1/ast"

	"github.com/open-policy-agent/eopa/pkg/builtins"
)

// used by go-generate to create capabilities.json file
func main() {
	// hook EOPA capabilities extensions callback
	builtins.Init()
	f := ast.CapabilitiesForThisVersion()

	fd, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}

	enc := json.NewEncoder(fd)
	enc.SetIndent("", "  ")

	if err := enc.Encode(f); err != nil {
		panic(err)
	}

	if err := fd.Close(); err != nil {
		panic(err)
	}
}
