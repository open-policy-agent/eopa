package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/topdown"
)

const (
	megabyte = 1073741824
)

func main() {
	var filename string
	var policy *ir.Policy
	fs := flag.NewFlagSet("irdump", flag.ExitOnError)
	fs.StringVar(&filename, "f", "", "Rego filename to read in and dump IR JSON for. (default: stdin)")
	fs.Parse(os.Args[1:])
	entrypoints := fs.Args()

	if len(entrypoints) == 0 {
		fs.Usage()
	}

	// Get input Rego file from stdin or a file on disk.
	var fileBytes bytes.Buffer
	if filename == "" {
		r := bufio.NewReaderSize(os.Stdin, megabyte)
		line, isPrefix, err := r.ReadLine()
		for err == nil {
			fileBytes.Write(line)
			if !isPrefix {
				fileBytes.WriteByte('\n')
			}
			line, isPrefix, err = r.ReadLine()
		}
		if err != io.EOF {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		b, err := os.ReadFile(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fileBytes.Write(b)
	}

	// Attempt to read as a bundle.
	br := bundle.NewCustomReader(bundle.NewTarballLoader(bytes.NewReader(fileBytes.Bytes()))).WithSkipBundleVerification(true)
	if b, err := br.Read(); err == nil {
		policy, err = compileBundle(topdown.BuiltinContext{}, &b, entrypoints)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		// Attempt to read as a normal Rego file.
		var err error
		policy, err = compileRego(topdown.BuiltinContext{}, filename, fileBytes.String(), entrypoints)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
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

func compileBundle(bctx topdown.BuiltinContext, b *bundle.Bundle, entrypointPaths []string) (*ir.Policy, error) {
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
