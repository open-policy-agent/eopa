package rego_vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	inmem "github.com/styrainc/load-private/pkg/store"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/test/cases"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

// topdown -> vm
var replacements = map[string]string{
	"functions must not produce multiple outputs for same inputs": "eval_conflict_error: complete rules must not produce multiple outputs",
	"object keys must be unique":                                  "object insert conflict",
}

func init() {
	addTestSleepBuiltin()
}

func readCases(t testing.TB) ([]cases.TestCase, map[string]string) {
	opaRootDir := os.Getenv("OPA_ROOT")
	if opaRootDir == "" {
		t.Skip("OPA_ROOT not set, skipping Rego E2E test/benchmarks")
	}
	opaRootDir = filepath.Clean(opaRootDir)
	caseDir := filepath.Join(opaRootDir, "test/cases/testdata")
	const exceptionsFile = "./exceptions.yaml"
	exceptions := map[string]string{}

	bs, err := os.ReadFile(exceptionsFile)
	if err != nil {
		t.Fatalf("Unable to load exceptions file: %v", err)
	}
	if err := util.Unmarshal(bs, &exceptions); err != nil {
		t.Fatalf("Unable to parse exceptions file: %v", err)
	}
	cs := cases.MustLoad(caseDir).Sorted().Cases
	for i := range cs {
		cs[i].Filename = strings.TrimPrefix(cs[i].Filename, opaRootDir)
	}
	return cs, exceptions
}

func TestRegoE2E(t *testing.T) {
	cases, exceptions := readCases(t)
	ctx := context.Background()

	for _, tc := range cases {
		name := tc.Filename + "/" + tc.Note
		t.Run(name, func(t *testing.T) {

			if shouldSkip(t, tc, exceptions) {
				t.SkipNow()
			}

			var store storage.Store
			if tc.Data != nil {
				store = inmem.NewFromObject(*tc.Data)
			} else {
				store = inmem.New()
			}

			opts := []func(*rego.Rego){
				rego.Query(tc.Query),
				rego.StrictBuiltinErrors(tc.StrictError),
				rego.Store(store),
			}
			for i := range tc.Modules {
				opts = append(opts, rego.Module(fmt.Sprintf("test-%d.rego", i), tc.Modules[i]))
			}
			if testing.Verbose() {
				opts = append(opts, rego.Dump(os.Stderr))
			}

			var input *ast.Term
			switch {
			case tc.InputTerm != nil:
				input = ast.MustParseTerm(*tc.InputTerm)
			case tc.Input != nil:
				input = ast.NewTerm(ast.MustInterfaceToValue(*tc.Input))
			}

			pq, err := rego.New(opts...).PrepareForEval(ctx)
			if err != nil {
				if tc.WantError != nil || tc.WantErrorCode != nil {
					assert(t, tc, nil, err)
				} else {
					t.Fatalf("tc: %v, err: %v", tc, err)
				}
			}

			var evalOpts []rego.EvalOption
			if input != nil {
				evalOpts = append(evalOpts, rego.EvalParsedInput(input.Value))
			}
			res, err := pq.Eval(ctx, evalOpts...)
			assert(t, tc, res, err)
		})
	}
}

// Benchmark  E2E tests
// to run all benchmark tests
//
//	go test -bench= -test.benchmem -timeout 6000s -run=^#
//
// to run single benchmark test:
//
//	go test -bench=BenchmarkRegoE2E//test/cases/testdata/withkeyword/test-withkeyword-1016.yaml/withkeyword -test.benchmem -timeout 6000s -run=^#
func BenchmarkRegoE2E(b *testing.B) {
	cases, exceptions := readCases(b)
	ctx := context.Background()

	for _, tc := range cases {
		name := tc.Filename + "/" + tc.Note
		b.Run(name, func(b *testing.B) {

			if shouldSkip(b, tc, exceptions) {
				b.SkipNow()
			}

			var store storage.Store
			if tc.Data != nil {
				store = inmem.NewFromObject(*tc.Data)
			} else {
				store = inmem.New()
			}

			opts := []func(*rego.Rego){
				rego.Query(tc.Query),
				rego.StrictBuiltinErrors(tc.StrictError),
				rego.Store(store),
			}
			for i := range tc.Modules {
				opts = append(opts, rego.Module(fmt.Sprintf("test-%d.rego", i), tc.Modules[i]))
			}
			if testing.Verbose() {
				opts = append(opts, rego.Dump(os.Stderr))
			}

			var input *ast.Term
			switch {
			case tc.InputTerm != nil:
				input = ast.MustParseTerm(*tc.InputTerm)
			case tc.Input != nil:
				input = ast.NewTerm(ast.MustInterfaceToValue(*tc.Input))
			}

			pq, err := rego.New(opts...).PrepareForEval(ctx)
			if err != nil {
				if tc.WantError != nil || tc.WantErrorCode != nil {
					assert(b, tc, nil, err)
				} else {
					b.Fatalf("tc: %v, err: %v", tc, err)
				}
			}

			var evalOpts []rego.EvalOption
			if input != nil {
				evalOpts = append(evalOpts, rego.EvalParsedInput(input.Value))
			}
			b.ResetTimer()

			// benchmark Evaluation phase
			for i := 0; i < b.N; i++ {
				res, err := pq.Eval(ctx, evalOpts...)
				assert(b, tc, res, err)
			}
		})
	}
}

func shouldSkip(t testing.TB, tc cases.TestCase, exceptions map[string]string) bool {
	if reason, ok := exceptions[tc.Note]; ok {
		t.Log("Skipping test case: " + reason)
		return true
	}

	return false
}

func assert(t testing.TB, tc cases.TestCase, result rego.ResultSet, err error) {

	if tc.WantError != nil {
		assertErrorText(t, *tc.WantError, err)
	}

	if tc.WantErrorCode != nil {
		assertErrorCode(t, *tc.WantErrorCode, err)
	}

	if err != nil && tc.WantErrorCode == nil && tc.WantError == nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tc.WantResult != nil {
		assertResultSet(t, *tc.WantResult, tc.SortBindings, result)
	}

	if tc.WantResult == nil && tc.WantErrorCode == nil && tc.WantError == nil {
		t.Fatal("expected one of: 'want_result', 'want_error_code', or 'want_error'")
	}
}

func assertResultSet(t testing.TB, want []map[string]interface{}, sortBindings bool, result rego.ResultSet) {
	exp := ast.NewSet()

	for _, b := range want {
		obj := ast.NewObject()
		for k, v := range b {
			obj.Insert(ast.StringTerm(k), ast.NewTerm(ast.MustInterfaceToValue(v)))
		}
		exp.Add(ast.NewTerm(obj))
	}

	got := ast.NewSet()

	for _, b := range result {
		obj := ast.NewObject()
		for k, v := range b.Bindings {
			if sortBindings {
				sort.Sort(resultSet(v.([]interface{})))
			}
			obj.Insert(ast.StringTerm(string(k)), ast.NewTerm(ast.MustInterfaceToValue(v)))
		}
		got.Add(ast.NewTerm(obj))
	}

	if exp.Compare(got) != 0 {
		t.Fatalf("unexpected query result:\nexp: %v\ngot: %v", exp, got)
	}
}

func assertErrorCode(t testing.TB, wantErrorCode string, err error) {
	e, ok := err.(*topdown.Error)
	if !ok {
		// Try known exception
		if err != nil && strings.Contains(err.Error(), "object insert conflict") {
			return
		}
		t.Fatalf("expected topdown error but got: %v %[1]T", err)
	}

	if e.Code != wantErrorCode {
		t.Fatalf("expected error code %q but got %q", wantErrorCode, e.Code)
	}
}

func assertErrorText(t testing.TB, wantText string, err error) {
	if err == nil {
		t.Fatal("expected error but got success")
	}
	// cut off source location
	colon := strings.Index(wantText, ": ")
	if colon > 0 {
		wantText = wantText[colon+2:]
	}
	if replacement, ok := replacements[wantText]; ok {
		wantText = replacement
	}
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("expected topdown error text %q but got: %q", wantText, err.Error())
	}
}

func addTestSleepBuiltin() {
	rego.RegisterBuiltin1(&rego.Function{
		Name: "test.sleep",
		Decl: types.NewFunction(types.Args(types.S), types.NewNull()),
	}, func(_ rego.BuiltinContext, op *ast.Term) (*ast.Term, error) {
		d, _ := time.ParseDuration(string(op.Value.(ast.String)))
		time.Sleep(d)
		return ast.NullTerm(), nil
	})
}

type resultSet []interface{}

func (rs resultSet) Less(i, j int) bool {
	return util.Compare(rs[i], rs[j]) < 0
}

func (rs resultSet) Swap(i, j int) {
	tmp := rs[i]
	rs[i] = rs[j]
	rs[j] = tmp
}

func (rs resultSet) Len() int {
	return len(rs)
}
