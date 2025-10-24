// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"

	eopa_bundle "github.com/open-policy-agent/eopa/pkg/plugins/bundle"
	eopa_vm "github.com/open-policy-agent/eopa/pkg/rego_vm"
)

func main() {

	ctx := context.Background()

	// Use EOPA's bundle activator, ensuring EOPA's default
	// storage layer will be used.
	a := &eopa_bundle.CustomActivator{}
	bundle.RegisterActivator("_eopa", a)
	bundle.RegisterDefaultBundleActivator("_eopa")

	// Construct a Rego object that can be prepared or evaluated.
	r := rego.New(
		rego.Query(os.Args[2]),
		rego.Load([]string{os.Args[1]}, nil),
		rego.Target(eopa_vm.Target),
	)

	// Create a prepared query that can be evaluated.
	query, err := r.PrepareForEval(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Load the input document from stdin.
	var input any
	dec := json.NewDecoder(os.Stdin)
	dec.UseNumber()
	if err := dec.Decode(&input); err != nil {
		log.Fatal(err)
	}

	// Execute the prepared query.
	rs, err := query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		log.Fatal(err)
	}

	// Do something with the result.
	fmt.Println(rs)
}
