package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/topdown"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage:\n\tirutil {json,dot} FILENAME ENTRYPOINT")
		os.Exit(1)
	}
	exportType := os.Args[1]
	entrypoints := os.Args[3:]

	filename := os.Args[2]
	b, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	policy, err := compileRego(topdown.BuiltinContext{}, filename, string(b), entrypoints)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	switch exportType {
	case "json":
		bs, err := json.Marshal(policy)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println(string(bs))
	case "dot":
		f, err := PolicyToCFGDAGForest(policy)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println(f.AsDOT())
	}
}

// Compiles a single Rego module to an ir.Policy.
func compileRego(bctx topdown.BuiltinContext, filename string, module string, entrypointPaths []string) (*ir.Policy, error) {
	parsed, err := ast.ParseModule(filename, module)
	if err != nil {
		return nil, err
	}

	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/",
				Path:   "/",
				Raw:    []byte(module),
				Parsed: parsed,
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(entrypointPaths...)
	if err := compiler.Build(bctx.Context); err != nil {
		return nil, err
	}

	bundle := compiler.Bundle()
	var ir ir.Policy
	if err := json.Unmarshal(bundle.PlanModules[0].Raw, &ir); err != nil {
		return nil, err
	}
	return &ir, nil
}
