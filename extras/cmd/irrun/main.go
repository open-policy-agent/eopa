package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	eopa_builtins "github.com/open-policy-agent/eopa/pkg/builtins"
	eopa_storage "github.com/open-policy-agent/eopa/pkg/storage"
	"github.com/open-policy-agent/eopa/pkg/vm"

	_ "github.com/open-policy-agent/eopa/pkg/plugins/bundle" // register bjson extension
)

const (
	megabyte = 1073741824
)

func main() {
	var regoIRFilename, inputFilename, dataFilename string
	var policy *ir.Policy
	fs := flag.NewFlagSet("irdump", flag.ExitOnError)
	fs.StringVar(&regoIRFilename, "f", "", "Rego IR JSON blob to read in and run. (default: stdin)")
	fs.StringVar(&inputFilename, "i", "", "JSON file to load as input to the policy.")
	fs.StringVar(&dataFilename, "d", "", "JSON file to load as data to the policy.")
	fs.Parse(os.Args[1:])
	query := fs.Arg(0)

	if query == "" {
		fmt.Fprintln(os.Stderr, "Needs 1 positional argument: query")
		fs.Usage()
		os.Exit(1)
	}

	// Get input Rego JSON IR file from stdin or a file on disk.
	var regoIRFileBytes bytes.Buffer
	if regoIRFilename == "" {
		r := bufio.NewReaderSize(os.Stdin, megabyte)
		line, isPrefix, err := r.ReadLine()
		for err == nil {
			regoIRFileBytes.Write(line)
			if !isPrefix {
				regoIRFileBytes.WriteByte('\n')
			}
			line, isPrefix, err = r.ReadLine()
		}
		if err != io.EOF {
			log.Fatal(err)
		}
	} else {
		b, err := os.ReadFile(regoIRFilename)
		if err != nil {
			log.Fatal(err)
		}
		regoIRFileBytes.Write(b)
	}

	if err := json.Unmarshal(regoIRFileBytes.Bytes(), &policy); err != nil {
		log.Fatal(err)
	}

	// Get the input JSON file, if one was specified.
	var inputFileBytes bytes.Buffer
	var rawInput interface{}
	if inputFilename != "" {
		b, err := os.ReadFile(inputFilename)
		if err != nil {
			log.Fatal(err)
		}
		inputFileBytes.Write(b)
		if err := json.Unmarshal(inputFileBytes.Bytes(), &rawInput); err != nil {
			log.Fatal(err)
		}
	}

	// Get the input JSON file, if one was specified.
	var dataFileBytes bytes.Buffer
	var rawData interface{}
	if dataFilename != "" {
		b, err := os.ReadFile(dataFilename)
		if err != nil {
			log.Fatal(err)
		}
		dataFileBytes.Write(b)
		if err := json.Unmarshal(dataFileBytes.Bytes(), &rawData); err != nil {
			log.Fatal(err)
		}
	}

	// We build up an accurate map of the builtins available in EOPA, much like
	// what would be fed into the PrepareForEval method in the rego_vm plugin
	// code.
	eopa_builtins.Init()
	bis := make(map[string]*topdown.Builtin, len(eopa_builtins.BuiltinMap))
	for _, bi := range eopa_builtins.BuiltinMap {
		bis[bi.Name] = &topdown.Builtin{
			Decl: ast.BuiltinMap[bi.Name],
			Func: topdown.GetBuiltin(bi.Name),
		}
	}

	// We build EOPA VM bytecode from an ir.Policy object, and also provide the
	// map of builtins from earlier.
	executable, err := vm.NewCompiler().WithPolicy(policy).WithBuiltins(bis).Compile()
	if err != nil {
		log.Fatal(err)
	}

	// This is where the VM kicks into action. We run the bytecode executable
	// with a particular query + set of eval parameters, and return the result
	// set from the VM, or an error if things blew up.
	_, ctx := vm.WithStatistics(context.Background()) // Note(philip): Necessary to avoid a panic from the VM. Stats are *mandatory*!
	result, err := Eval(ctx, executable, bis, query, &vm.EvalOpts{Input: &rawInput}, rawData)
	if err != nil {
		log.Fatal(err)
	}

	bs, err := json.Marshal(result)
	if err != nil {
		log.Fatal(err)
	}

	// Dump result set to stdout.
	fmt.Println(string(bs))
}

// Note(philip): This is a slightly reworked version of the (*vme).Eval(...)
// method from `pkg/rego_vm/plugin.go`. It changes the parameters to allow
// tighter control over everything going into the VM's evaluation context, and
// also (for now) removes stats/metrics collection.
func Eval(ctx context.Context, executable vm.Executable, builtinFuncs map[string]*topdown.Builtin, query string, eopts *vm.EvalOpts, data interface{}) (ast.Value, error) {
	v := vm.NewVM().WithExecutable(executable)
	// var span trace.Span
	// ctx, span = spanFromContext(ctx, ectx.CompiledQuery().String())
	// defer span.End()

	input := eopts.Input

	// ectx.Metrics().Timer(evalTimer).Start()
	// var s *vm.Statistics
	// s, ctx = vm.WithStatistics(ctx)

	seed := eopts.Seed
	if seed == nil {
		seed = rand.Reader
	}

	// NOTE(sr): We're peeking into the transaction to cover cases where we've been fed a
	// default OPA inmem store, not an EOPA one. If that's the case, we'll read it in full,
	// and feed its data to the VM. That will have subtle differences in behavior; but it
	// is good enough for the remaining cases where this is allowed to happen: discovery
	// document evaluation.
	store := eopa_storage.New()
	if data != nil {
		store = eopa_storage.NewFromObject(data)
	}
	txn := storage.NewTransactionOrDie(ctx, store, storage.WriteParams)
	v = v.WithDataNamespace(txn)

	result, err := v.Eval(ctx, query, vm.EvalOpts{
		// Metrics:                eopts.Metrics(),
		Input:                  input,
		Time:                   eopts.Time,
		Seed:                   seed,
		Runtime:                eopts.Runtime,
		Cache:                  builtins.Cache{},
		NDBCache:               eopts.NDBCache,
		InterQueryBuiltinCache: eopts.InterQueryBuiltinCache,
		PrintHook:              eopts.PrintHook,
		StrictBuiltinErrors:    eopts.StrictBuiltinErrors,
		Capabilities:           eopts.Capabilities,
		// TracingOpts:            tracingOpts(eopts),
		Limits:       &vm.DefaultLimits,
		BuiltinFuncs: builtinFuncs,
	})
	// ectx.Metrics().Timer(evalTimer).Stop()
	if err != nil {
		if err == vm.ErrVarAssignConflict {
			return nil, &topdown.Error{
				Code:    topdown.ConflictErr,
				Message: "complete rules must not produce multiple outputs",
			}
		}

		return nil, err
	}
	// statsToMetrics(ectx.Metrics(), s)

	return result, nil
}
