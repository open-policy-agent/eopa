package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/topdown"
)

func main() {
	var filename string
	fs := flag.NewFlagSet("irdump", flag.ExitOnError)
	fs.StringVar(&filename, "f", "", "Rego filename to read in and dump IR JSON for. (default: stdin)")
	fs.Parse(os.Args[1:])
	entrypoints := fs.Args()

	if len(entrypoints) == 0 {
		fs.Usage()
	}

	// Get input Rego file from stdin or a file on disk.
	var fileText strings.Builder
	if filename == "" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fileText.WriteString(scanner.Text() + "\n")
		}
	} else {
		b, err := os.ReadFile(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fileText.Write(b)
	}

	policy, err := compileRego(topdown.BuiltinContext{}, filename, fileText.String(), entrypoints)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	bs, err := json.Marshal(policy)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(string(bs))
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
