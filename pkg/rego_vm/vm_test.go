package rego_vm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"

	regovm "github.com/styrainc/load/pkg/rego_vm/vm"
	"github.com/styrainc/load/pkg/vm"
)

func setup(tb testing.TB, rego string, query string) ir.Policy {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(rego),
				Parsed: ast.MustParseModule(rego),
			},
		},
	}

	// use OPA to extract IR
	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	bundle := compiler.Bundle()
	var policy ir.Policy

	if err := json.Unmarshal(bundle.PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}
	return policy
}

func testCompiler(tb testing.TB, policy ir.Policy, input string, query string, result string) {
	ctx := context.Background()

	ops := regovm.NewDataOperations()
	executable, err := vm.NewCompiler(ops).WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx = vm.WithStatistics(ctx)

	var inp interface{} = ast.MustParseTerm(input)

	nvm := vm.NewVM().WithDataOperations(ops).WithExecutable(executable)
	v, err := nvm.Eval(ctx, query, vm.EvalOpts{
		Input: &inp,
		Time:  time.Now(),
	})
	if err != nil {
		tb.Fatal(err)
	}
	if result == "" {
		return
	}

	matchResult(tb, result, v)
}

func matchResult(tb testing.TB, result string, v vm.Value) {
	x, ok := v.(ast.Value)
	if !ok {
		tb.Fatalf("invalid conversion to ast.Value")
	}
	t := ast.MustParseTerm(result)
	if x.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, result)
	}
}

const simpleRego = "package test\nallow := x {\nx := true}"
const simpleInput = "{}"
const simpleQuery = "test/allow"
const simpleResult = `{{"result": true}}`

func TestCompiler(t *testing.T) {
	policy := setup(t, simpleRego, simpleQuery)
	testCompiler(t, policy, simpleInput, simpleQuery, simpleResult)
}

func BenchmarkCompiler(b *testing.B) {
	policy := setup(b, simpleRego, simpleQuery)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		testCompiler(b, policy, simpleInput, simpleQuery, "")
	}
}

func TestMonster(t *testing.T) {
	rego, err := os.ReadFile("testdata/monster.rego")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile("testdata/monster.input")
	if err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile("testdata/monster.result")
	if err != nil {
		t.Fatal(err)
	}
	query := "play"
	policy := setup(t, string(rego), query)
	testCompiler(t, policy, string(input), query, string(result))
}

func BenchmarkMonster(b *testing.B) {
	rego, err := os.ReadFile("monster.rego")
	if err != nil {
		b.Fatal(err)
	}
	input, err := os.ReadFile("monster.input")
	if err != nil {
		b.Fatal(err)
	}
	query := "play"
	policy := setup(b, string(rego), query)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		testCompiler(b, policy, string(input), query, "")
	}
}
