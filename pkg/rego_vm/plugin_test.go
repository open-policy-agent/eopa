package rego_vm_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/rego"
	opa_storage "github.com/open-policy-agent/opa/v1/storage"
	opa_inmem "github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
	"github.com/open-policy-agent/opa/v1/types"

	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
	"github.com/styrainc/enterprise-opa-private/pkg/storage"
)

// For EOPA, this test is twofold:
// glob.match is handled by an overridden builtin implementation, so it manages the caching itself;
// regex.match is not -- so we're also checking that the plumbing rego -> vm -> topdown works
func TestEvalWithInterQueryValueCache(t *testing.T) {
	vm := rego.Target(rego_vm.Target)
	ctx := context.Background()
	// add an inter-query value cache
	config, _ := cache.ParseCachingConfig(nil)
	interQueryValueCache := cache.NewInterQueryValueCache(ctx, config)
	m := metrics.New()
	query := `regex.match("foo.*", "foobar")`
	_, err := rego.New(vm, rego.Query(query), rego.InterQueryBuiltinValueCache(interQueryValueCache), rego.Metrics(m)).Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// eval again with same query
	// this request should be served by the cache
	_, err = rego.New(vm, rego.Query(query), rego.InterQueryBuiltinValueCache(interQueryValueCache), rego.Metrics(m)).Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exp, act := uint64(1), m.Counter("rego_builtin_regex_interquery_value_cache_hits").Value(); exp != act {
		t.Fatalf("expected %d cache hits, got %d", exp, act)
	}
	query = `glob.match("*.example.com", ["."], "api.example.com")`
	_, err = rego.New(vm, rego.Query(query), rego.InterQueryBuiltinValueCache(interQueryValueCache), rego.Metrics(m)).Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// eval again with same query
	// this request should be served by the cache
	_, err = rego.New(vm, rego.Query(query), rego.InterQueryBuiltinValueCache(interQueryValueCache), rego.Metrics(m)).Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rego.New(vm, rego.Query(query), rego.InterQueryBuiltinValueCache(interQueryValueCache), rego.Metrics(m)).Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exp, act := uint64(2), m.Counter("rego_builtin_glob_interquery_value_cache_hits").Value(); exp != act {
		t.Fatalf("expected %d cache hits, got %d", exp, act)
	}
}

func TestNDBCache(t *testing.T) {
	ndbc := builtins.NDBCache{}
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Query("data.x.p = x"),
		rego.Module("test.rego", `package x
import rego.v1
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
				rego.Target(rego_vm.Target),
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

	type (
		xt map[string][]string
		yt []bool
		zt string
		ut [2]bool
		wt struct {
			Foo string `json:"foo"`
		}
	)

	now := time.Now()
	bs, _ := now.MarshalText()
	expTS := string(bs)

	err := ast.NewError(
		ast.TypeErr,
		ast.NewLocation([]byte(""), "foo.rego", 1, 2),
		"something went wrong: %s",
		"ohno",
	)
	err.Details = &ast.RefErrInvalidDetail{
		Ref:   ast.MustParseRef("data[1]"),
		Pos:   1,
		Have:  types.N,
		OneOf: []ast.Value{ast.String("system")},
	}

	tests := []struct {
		note  string
		input any
		exp   any
		vmExp any // known discrepancies between rego and VM result
	}{
		{
			note:  "custom type, map",
			input: xt(map[string][]string{"foo": {"bar", "baz"}}),
			exp:   map[string]any{"foo": []any{"bar", "baz"}},
		},
		{
			note:  "custom type, slice",
			input: yt([]bool{true, true, false}),
			exp:   []any{true, true, false},
		},
		{
			note:  "custom type, string",
			input: zt("foo"),
			exp:   any("foo"),
		},
		{
			note:  "custom type, array",
			input: ut([2]bool{true, true}),
			exp:   []any{true, true},
		},
		{
			note:  "custom type, struct",
			input: wt{Foo: "fox"},
			exp:   map[string]any{"foo": "fox"},
		},
		{
			note:  "custom type, struct ptr",
			input: &wt{Foo: "fox"},
			exp:   map[string]any{"foo": "fox"},
		},
		{
			note:  "time.Time",
			input: map[string]any{"now": now},
			exp:   map[string]any{"now": expTS},
		},
		{ // DL feeds errors into the mask/drop eval
			// So the concrete representation as the VM sees it is not super important:
			// drop reduces its into to a boolean; mask to a set of json pointers.
			// It's very unlikely that someone worries much about specific nested error details
			// when masking.
			note:  "ast errors",
			input: map[string]any{"error": err},
			exp: map[string]any{
				"error": map[string]any{
					"code": "rego_type_error",
					"details": map[string]any{
						"ref": []any{
							map[string]any{"type": "var", "value": "data"},
							map[string]any{"type": "number", "value": json.Number("1")},
						},
						"pos":  json.Number("1"),
						"have": map[string]any{"type": "number"},
						"want": nil,

						"oneOf": []any{"system"},
					},
					"location": map[string]any{
						"col":  json.Number("2"),
						"file": "foo.rego",
						"row":  json.Number("1"),
					},
					"message": "something went wrong: ohno",
				},
			},
			vmExp: map[string]any{
				"error": map[string]any{
					"code": "rego_type_error",
					"details": map[string]any{
						"ref":  []any{"data", json.Number("1")}, // ast.Ref -> []*ast.Term -> []any
						"pos":  json.Number("1"),
						"have": map[string]any{}, // NOTE(sr): I don't know why this happens.
						"want": nil,

						"oneOf": []any{"system"},
					},
					"location": map[string]any{
						"col":  json.Number("2"),
						"file": "foo.rego",
						"row":  json.Number("1"),
					},
					"message": "something went wrong: ohno",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {

			for _, tgt := range []string{"rego", rego_vm.Target} {
				t.Run(tgt, func(t *testing.T) {
					r := rego.New(
						rego.Target(tgt),
						rego.Query("result = input"),
					)
					pr, err := r.PrepareForEval(ctx)
					if err != nil {
						t.Fatal(err)
					}
					res, err := pr.Eval(ctx, rego.EvalInput(tc.input))
					if err != nil {
						t.Fatal(err)
					}
					act := res[0].Bindings["result"]
					exp := tc.exp
					if tc.vmExp != nil && tgt == rego_vm.Target {
						exp = tc.vmExp
					}
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Errorf("unexpected result (-want, +got):\n%s", diff)
					}
				})
			}

		})
	}
}

func TestEvalWithFuncMemoization(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		note         string
		mod          string
		input        any
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
			note:  "one argument/string from literal+var+input",
			input: "a",
			mod: `
f(_) := 1
p if { # miss
	f("a")   # miss
	f("a")   # hit
	x := "a"
	f(x)     # hit
	f(input) # hit
}`,
			misses: 2,
			hits:   3,
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
import rego.v1
` + tc.mod

			m := metrics.New()
			opts := []func(*rego.Rego){
				rego.Target(rego_vm.Target),
				rego.Query("data.test.p = x"),
				rego.Module("test.rego", policy),
				rego.Metrics(m),
			}
			if tc.input != nil {
				opts = append(opts, rego.Input(tc.input))
			}
			r := rego.New(opts...)
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
