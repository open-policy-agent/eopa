package rego_vm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func setup(tb testing.TB, b *bundle.Bundle, query string) ir.Policy {
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

func createBundle(_ testing.TB, rego string) *bundle.Bundle {
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
	return b
}

func loadBundle(tb testing.TB, buffer []byte) *bundle.Bundle {
	reader := bundle.NewCustomReader(bundle.NewTarballLoader(bytes.NewReader(buffer)))
	b, err := reader.Read()
	if err != nil {
		tb.Fatal("bundle failed", err)
	}
	return &b
}

func testCompiler(tb testing.TB, policy ir.Policy, input string, query string, result string, data any) func(testing.TB) {
	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	var inp interface{} = ast.MustParseTerm(input)
	bdata := bjson.MustNew(data)
	nvm := vm.NewVM().WithExecutable(executable).WithDataJSON(bdata)

	return func(tb testing.TB) {
		s, ctx := vm.WithStatistics(context.Background())
		v, err := nvm.Eval(ctx, query, vm.EvalOpts{
			Input: &inp,
			Time:  time.Now(),
		})
		if err != nil {
			tb.Fatal(err)
		}
		if b, ok := tb.(*testing.B); ok {
			b.ReportMetric(float64(s.EvalInstructions), "instr/op")
		}
		if result == "" {
			return
		}

		matchResult(tb, result, v)
	}
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
	bundle := createBundle(t, simpleRego)
	policy := setup(t, bundle, simpleQuery)
	testCompiler(t, policy, simpleInput, simpleQuery, simpleResult, bundle.Data)(t)
}

func BenchmarkCompiler(b *testing.B) {
	bundle := createBundle(b, simpleRego)
	policy := setup(b, bundle, simpleQuery)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testCompiler(b, policy, simpleInput, simpleQuery, "", bundle.Data)(b)
	}
}

func BenchmarkCompilerEval(b *testing.B) {
	bundle := createBundle(b, simpleRego)
	policy := setup(b, bundle, simpleQuery)
	f := testCompiler(b, policy, simpleInput, simpleQuery, "", bundle.Data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f(b)
	}
}

//go:embed testdata/monster.rego
var benchMonsterRego string

//go:embed testdata/monster.input
var benchMonsterInput string

//go:embed testdata/monster.result
var benchMonsterResult string

func TestMonster(t *testing.T) {
	query := "play"
	bundle := createBundle(t, benchMonsterRego)
	policy := setup(t, bundle, query)
	testCompiler(t, policy, benchMonsterInput, query, benchMonsterResult, bundle.Data)(t)
}

func BenchmarkMonster(b *testing.B) {
	query := "play"
	bundle := createBundle(b, benchMonsterRego)
	policy := setup(b, bundle, query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testCompiler(b, policy, benchMonsterInput, query, "", bundle.Data)(b)
	}
}

func BenchmarkMonsterEval(b *testing.B) {
	query := "play"
	bundle := createBundle(b, benchMonsterRego)
	policy := setup(b, bundle, query)
	f := testCompiler(b, policy, benchMonsterInput, query, "", bundle.Data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f(b)
	}
}

// Entitlements
//
//go:embed testdata/entitlements.input
var benchEntitlementsInput string

//go:embed testdata/entitlements.result
var benchEntitlementsResult string

//go:embed testdata/entitlements.bundle.tgz
var benchEntitlementsBundle []byte

var benchEntitlementsQuery = `main/main`

func TestEntitlements(t *testing.T) {
	bundle := loadBundle(t, benchEntitlementsBundle)
	policy := setup(t, bundle, benchEntitlementsQuery)
	testCompiler(t, policy, benchEntitlementsInput, benchEntitlementsQuery, benchEntitlementsResult, bundle.Data)(t)
}

func BenchmarkEntitlements(b *testing.B) {
	bundle := loadBundle(b, benchEntitlementsBundle)
	policy := setup(b, bundle, benchEntitlementsQuery)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testCompiler(b, policy, benchEntitlementsInput, benchEntitlementsQuery, "", bundle.Data)(b)
	}
}

func BenchmarkEntitlementsEval(b *testing.B) {
	bundle := loadBundle(b, benchEntitlementsBundle)
	policy := setup(b, bundle, benchEntitlementsQuery)
	f := testCompiler(b, policy, benchEntitlementsInput, benchEntitlementsQuery, "", bundle.Data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f(b)
	}
}
