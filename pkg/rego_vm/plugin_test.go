package rego_vm_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/rego"
	opa_storage "github.com/open-policy-agent/opa/storage"
	opa_inmem "github.com/open-policy-agent/opa/storage/inmem"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"

	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
	"github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestNDBCache(t *testing.T) {
	ndbc := builtins.NDBCache{}
	r := rego.New(
		rego.Query("data.x.p = x"),
		rego.Module("test.rego", `package x
import future.keywords.if
p := rand.intn("x", 2) if numbers.range(1, 2)`), // only one of the builtins is non-det
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

// TestStorageTransactionRead asserts that something added to the inflight txn
// after PrepareForEval is consulted during the subsequent Eval.
// We're testing both the EOPA inmem storage and the OPA inmem store: The latter
// is only used in edge cases (intermediate evals of discovery configs).
func TestStorageTransactionRead(t *testing.T) {
	ctx := context.Background()
	for n, tc := range map[string]opa_storage.Store{
		"opa_inmem":    opa_inmem.New(),
		"eopa_storage": storage.New(),
	} {
		t.Run(n, func(t *testing.T) {
			store := tc
			txn := opa_storage.NewTransactionOrDie(ctx, store, opa_storage.WriteParams)
			defer store.Abort(ctx, txn)

			pq, err := rego.New(
				rego.Store(store),
				rego.Transaction(txn),
				rego.Query("data.foo = x"),
			).PrepareForEval(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if err := store.Write(ctx, txn, opa_storage.AddOp, []string{"foo"}, 3); err != nil {
				t.Fatal(err)
			}

			res, err := pq.Eval(ctx, rego.EvalTransaction(txn))
			if err != nil {
				t.Fatal(err)
			}
			if exp, act := 1, len(res); exp != act {
				t.Fatalf("expected %d result(s), got %d", exp, act)
			}
			if exp, act := json.Number("3"), res[0].Bindings["x"]; exp != act {
				t.Errorf("expected x bound to %v %[1]T, got %v %[2]T", exp, act)
			}
		})
	}
}

var RegalLastMeta = &rego.Function{
	Name: "regal.last",
	Decl: types.NewFunction(
		types.Args(
			types.Named("array", types.NewArray(nil, types.A)).
				Description("performance optimized last index retrieval"),
		),
		types.Named("element", types.A),
	),
}

// RegalLast regal.last returns the last element of an array.
func RegalLast(_ rego.BuiltinContext, arr *ast.Term) (*ast.Term, error) {
	arrOp, err := builtins.ArrayOperand(arr.Value, 1)
	if err != nil {
		return nil, err
	}

	if arrOp.Len() == 0 {
		return nil, errors.New("out of bounds")
	}

	return arrOp.Elem(arrOp.Len() - 1), nil
}

func TestRegoFunction(t *testing.T) {
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Query("data.x.p = x"),
		rego.Module("test.rego", `package x
p := regal.last(numbers.range(0, 10))
`),
		rego.Function1(RegalLastMeta, RegalLast),
	)
	res, err := r.Eval(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if exp, act := 1, len(res); exp != act {
		t.Errorf("expected %d results, got %d", exp, act)
	}
	if exp, act := json.Number("10"), res[0].Bindings["x"]; exp != act {
		t.Errorf("expected x bound to %v %[1]T, got %v %[2]T", exp, act)
	}
}

func TestPrepareForEvalRace(t *testing.T) {
	ctx := context.Background()
	const count = 100
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Query("data.x.p = x"),
		rego.Module("test.rego", `package x
p := numbers.range(0, 10)
`))
	pr, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wg := sync.WaitGroup{}
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			_, err := pr.Eval(ctx)
			if err != nil {
				panic(err)
			}
		}()
	}
	wg.Wait()
}

func TestEvalInputWithCustomType(t *testing.T) {
	ctx := context.Background()

	type xt map[string][]string
	var x xt = map[string][]string{"foo": {"bar", "baz"}}
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Query("result = input"),
		rego.Input(x),
	)

	pr, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := pr.Eval(ctx, rego.EvalInput(x))
	if err != nil {
		t.Fatal(err)
	}
	exp := map[string]any{"foo": []any{"bar", "baz"}}
	act := res[0].Bindings["result"]
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("unexpected result (-want, +got):\n%s", diff)
	}
}

func TestEvalWithFuncMemoization(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		note         string
		mod          string
		hits, misses uint64
	}{
		{
			note: "one argument/int",
			mod: `
f(_) := 1
p if { # miss
	f(1) # miss
	f(1) # hit
}`,
			misses: 2,
			hits:   1,
		},
		{
			note: "two arguments/int,string",
			mod: `
f(_, _) := 1
p if { # miss
	f(1, "foo") # miss
	f(1, "foo") # hit
}`,
			misses: 2,
			hits:   1,
		},
		{
			note: "nine arguments/int,string,bool,null,float,string,string,string,string",
			mod: `
f(_1, _2, _3, _4, _5, _6, _7, _8, _9) := 1
p if { # miss
	f(1, "foo", true, null, 1.1, "a", "b", "c", "d") # miss
	f(1, "foo", true, null, 1.1, "a", "b", "c", "d") # hit
}`,
			misses: 2,
			hits:   1,
		},
		{
			note: "ten arguments/ints",
			mod: `
f(_1, _2, _3, _4, _5, _6, _7, _8, _9, _10) := 1
p if { # miss
	f(1, 2, 3, 4, 5, 6, 7, 8, 9, 10) # miss
	f(1, 2, 3, 4, 5, 6, 7, 8, 9, 10) # miss -- 10+ args not supported
}`,
			misses: 3,
			hits:   0,
		},
		{
			note: "one argument/object",
			mod: `
f(_) := 1
p if { # miss
	f({"a":1}) # miss
	f({"a":1}) # miss -- not supported yet
}`,
			misses: 3,
			hits:   0,
		},
		{
			note: "one argument/array",
			mod: `
f(_) := 1
p if { # miss
	f([1]) # miss
	f([1]) # miss -- not supported yet
}`,
			misses: 3,
			hits:   0,
		},
		{
			note: "one argument/set",
			mod: `
f(_) := 1
p if { # miss
	f({1}) # miss
	f({1}) # miss -- not supported yet
}`,
			misses: 3,
			hits:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			policy := `package test
import future.keywords
` + tc.mod

			m := metrics.New()
			r := rego.New(
				rego.Target(rego_vm.Target),
				rego.Query("data.test.p = x"),
				rego.Module("test.rego", policy),
				rego.Metrics(m),
			)
			res, err := r.Eval(ctx)
			if err != nil {
				t.Fatal(err)
			}
			m0 := m.All()
			if exp, act := 1, len(res); exp != act {
				t.Fatalf("expected %d results, got %d", exp, act)
			}
			if exp, act := (rego.Vars{"x": true}), res[0].Bindings; cmp.Diff(exp, act) != "" {
				t.Fatalf("unexpected result (-want, +got):\n%s", cmp.Diff(exp, act))
			}
			if exp, act := tc.hits, m0["counter_regovm_virtual_cache_hits"]; exp != act {
				t.Errorf("expected %d hits, got %d", exp, act)
			}
			if exp, act := tc.misses, m0["counter_regovm_virtual_cache_misses"]; exp != act {
				t.Errorf("expected %d misses, got %d", exp, act)
			}
		})
	}
}
