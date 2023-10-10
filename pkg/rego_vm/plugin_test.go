package rego_vm_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/ast"
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
