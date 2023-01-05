package vm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
)

func setup(tb testing.TB) ir.Policy {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte("package test\nallow := x {\nx := true}"),
				Parsed: ast.MustParseModule("package test\nallow := x {\nx := true}"),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints("test/allow")
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

func testCompiler(tb testing.TB, policy ir.Policy) {
	ops := &testDataOperations{}
	_, err := NewCompiler(ops).WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}
}

type testDataOperations struct {
	DataOperations
}

func TestCompiler(t *testing.T) {
	policy := setup(t)
	testCompiler(t, policy)
}

func BenchmarkCompiler(b *testing.B) {
	policy := setup(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		testCompiler(b, policy)
	}
}
