package vm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/ir"
)

func setup(tb testing.TB) ir.Policy {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte("package test\nallow := x if {\nx := true}"),
				Parsed: ast.MustParseModule("package test\nallow := x if {\nx := true}"),
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
	if _, err := NewCompiler().WithPolicy(&policy).Compile(); err != nil {
		tb.Fatal(err)
	}
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
