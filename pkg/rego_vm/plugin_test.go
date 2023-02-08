package rego_vm_test

import (
	"context"
	"testing"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func TestNDBCache(t *testing.T) {
	ndbc := builtins.NDBCache{}
	r := rego.New(
		rego.Query("data.x.p = x"),
		rego.Module("test.rego", `package x
p := rand.intn("x", 2)`),
		rego.NDBuiltinCache(ndbc),
	)
	res, err := r.Eval(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if exp, act := 1, len(res); exp != act {
		t.Errorf("expected %d results, got %d", exp, act)
	}
	if exp, act := 1, len(ndbc); exp != act {
		t.Fatalf("expected %d cached entries, got %d", exp, act)
	}
	v, ok := ndbc.Get("rand.intn", ast.NewArray(ast.StringTerm("x"), ast.NumberTerm("2")))
	if !ok {
		t.Fatalf("expected \"rand.intn\" entry, got %v", ndbc)
	}
	if v.Compare(ast.Number("0")) != 0 && v.Compare(ast.Number("1")) != 0 {
		t.Errorf("expected value 0 or 1, got %v", v)
	}
}
