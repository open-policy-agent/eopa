// Package rego_test demonstrates the use of OPA's rego package,
// github.com/open-policy-agent/opa/rego, with EOPA's VM code.
package rego_test

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
	"github.com/styrainc/enterprise-opa-private/pkg/storage"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/rego"
	opa_storage "github.com/open-policy-agent/opa/storage"
	opa_inmem "github.com/open-policy-agent/opa/storage/inmem"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

func ExampleRego_Eval_simple_default() { // Start with the default rego package
	ctx := context.Background()

	// Create very simple query that binds a single variable.
	r := rego.New(rego.Query("x = 1"))

	// Run evaluation.
	rs, err := r.Eval(ctx)
	if err != nil {
		panic(err)
	}

	// Inspect results.
	fmt.Println("len:", len(rs))
	fmt.Println("bindings:", rs[0].Bindings)

	// Output:
	//
	// len: 1
	// bindings: map[x:1]
}

func ExampleRego_Eval_simple_load() { // Repeat the same with the EOPA VM instead:
	ctx := context.Background()

	r := rego.New(
		rego.Query("x = 1"),
		rego.Target(rego_vm.Target), // This is new!
	)

	// Run evaluation.
	rs, err := r.Eval(ctx)
	if err != nil {
		panic(err)
	}

	// Inspect results.
	fmt.Println("len:", len(rs))
	fmt.Println("bindings:", rs[0].Bindings)

	// Output:
	//
	// len: 1
	// bindings: map[x:1]
}

const data = `{
  "example": {
    "users": [
      {
        "name": "alice",
        "likes": [
          "dogs",
          "clouds"
        ]
      },
      {
        "name": "bob",
        "likes": [
          "pizza",
          "cats"
        ]
      }
    ]
  }
}`

func ExampleRego_Eval_storage_default() { // This is using OPA's inmem storage.
	ctx := context.Background()

	json := util.MustUnmarshalJSON([]byte(data))

	// Manually create the storage layer. inmem.NewFromObject returns an
	// in-memory store containing the supplied data.
	store := opa_inmem.NewFromObject(json.(map[string]any))

	// Create new query that returns the value
	r := rego.New(
		rego.Query("data.example.users[0].likes"),
		rego.Store(store),
	)

	// Run evaluation.
	rs, err := r.Eval(ctx)
	if err != nil {
		panic(err)
	}

	// Inspect the result.
	fmt.Println("value:", rs[0].Expressions[0].Value)

	// Output:
	//
	// value: [dogs clouds]
}

func ExampleRego_Eval_storage_bjson() { // This is using EOPA's optimized inmem storage.
	ctx := context.Background()

	json := util.MustUnmarshalJSON([]byte(data))

	store := storage.NewFromObject(json) // instantiate BJSON inmem storage

	r := rego.New(
		rego.Query("data.example.users[0].likes"),
		rego.Store(store),
		rego.Target(rego_vm.Target),
	)

	// Run evaluation.
	rs, err := r.Eval(ctx)
	if err != nil {
		panic(err)
	}

	// Inspect the result.
	fmt.Println("value:", rs[0].Expressions[0].Value)

	// Output:
	//
	// value: [dogs clouds]
}

func ExampleRego_Eval_storage_bjson_bundle() { // Loading a BJSON bundle into the store.
	ctx := context.Background()

	input := util.MustUnmarshalJSON([]byte(`{"action": "create", "user": "alice"}`))

	store := storage.New()
	txn := opa_storage.NewTransactionOrDie(ctx, store, opa_storage.WriteParams)

	r := rego.New(
		rego.Query("data.test.allow"),
		rego.Store(store),
		rego.Transaction(txn),
		rego.LoadBundle("testdata/bundle.bjson.tar.gz"),
		rego.Input(input),
		rego.Target(rego_vm.Target),
	)

	// Run evaluation.
	rs, err := r.Eval(ctx)
	if err != nil {
		panic(err)
	}

	// Inspect the result.
	fmt.Println("value:", rs[0].Expressions[0].Value)

	// Output:
	//
	// value: true
}

func ExampleRego_PrepareForEval() { // This is also supported!
	ctx := context.Background()

	// Create a simple query
	r := rego.New(
		rego.Query("input.x == 1"),
		rego.Target(rego_vm.Target),
	)

	// Prepare for evaluation
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		panic(err)
	}

	// Raw input data that will be used in the first evaluation
	input := map[string]interface{}{"x": 2}

	// Run the evaluation
	rs, err := pq.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		panic(err)
	}

	// Inspect results.
	fmt.Println("initial result:", rs[0].Expressions[0])

	// Update input
	input["x"] = 1

	// Run the evaluation with new input
	rs, err = pq.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		panic(err)
	}

	// Inspect results.
	fmt.Println("updated result:", rs[0].Expressions[0])

	// Output:
	//
	// initial result: false
	// updated result: true
}

func ExampleRego_custom_function_global() {

	decl := &rego.Function{
		Name: "trim_and_split",
		Decl: types.NewFunction(
			types.Args(types.S, types.S), // two string inputs
			types.NewArray(nil, types.S), // variable-length string array output
		),
	}
	impl := func(_ rego.BuiltinContext, a, b *ast.Term) (*ast.Term, error) {

		str, ok1 := a.Value.(ast.String)
		delim, ok2 := b.Value.(ast.String)

		// The function is undefined for non-string inputs. Built-in
		// functions should only return errors in unrecoverable cases.
		if !ok1 || !ok2 {
			return nil, nil
		}

		result := strings.Split(strings.Trim(string(str), string(delim)), string(delim))

		arr := make([]*ast.Term, len(result))
		for i := range result {
			arr[i] = ast.StringTerm(result[i])
		}

		return ast.ArrayTerm(arr...), nil
	}

	// The rego package exports helper functions for different arities and a
	// special version of the function that accepts a dynamic number.
	rego.RegisterBuiltin2(decl, impl)

	r := rego.New(
		rego.Target(rego_vm.Target),
		// An example query that uses a custom function.
		rego.Query(`x = trim_and_split("/foo/bar/baz/", "/")`),
	)

	rs, err := r.Eval(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println(rs[0].Bindings["x"])

	// Output:
	//
	// [foo bar baz]
}

func ExampleRego_print_statements() {

	var buf bytes.Buffer

	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Query("data.example.rule_containing_print_call"),
		rego.Module("example.rego", `
			package example

			rule_containing_print_call {
				print("input.foo is:", input.foo, "and input.bar is:", input.bar)
			}
		`),
		rego.Input(map[string]interface{}{
			"foo": 7,
		}),
		rego.EnablePrintStatements(true),
		rego.PrintHook(topdown.NewPrintHook(&buf)),
	)

	_, err := r.Eval(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println("buf:", buf.String())

	// Output:
	//
	// buf: input.foo is: 7 and input.bar is: <undefined>
}

func ExampleRego_metrics() {
	m := metrics.New()
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Metrics(m),
		rego.Query("_ = numbers.range(1, 100)"),
	)

	_, err := r.Eval(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println("number of metrics:", len(m.All()))
	fmt.Println("vm evaluated instruction count:", m.All()["counter_regovm_eval_instructions"])

	// Output:
	//
	// number of metrics: 6
	// vm evaluated instruction count: 8
}

func ExampleRego_instruction_limit() {
	rego_vm.SetLimits(32)
	defer rego_vm.SetLimits(100_000_000)

	d := make([]any, 100)
	for i := range d {
		d[i] = map[string]any{strconv.Itoa(i): i}
	}
	store := storage.NewFromObject(map[string]any{"lots": d})
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Store(store),
		rego.Query("i = data.lots[i][i]"),
	)

	_, err := r.Eval(context.Background())
	if err != nil {
		fmt.Printf("err: %s", err.Error())
		return
	}

	// Output:
	//
	// err: instructions limit exceeded
}
