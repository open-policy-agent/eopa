package builtins

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins/rego"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestRegoEval(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
		intraQueryCacheHits int
	}{
		{
			note:                "missing parameter(s)",
			source:              `p := rego.eval({})`,
			result:              "",
			error:               `eval_type_error: rego.eval: operand 1 missing required request parameters(s): {"path"}`,
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "a basic eval",
			source:              `p := rego.eval({"input": true, "module": "package foo.bar\nx := input", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note: "intra-query query cache",
			source: `p = [ resp1, resp2 ] if {
			     rego.eval({"module": "package foo.bar\nx := true", "path": "foo.bar.x"}, resp1)
                             rego.eval({"module": "package foo.bar\nx := true", "path": "foo.bar.x"}, resp2) # cached
                        }`,
			result:              `{{"result": {"p": [{{"result": true}}, {{"result": true}}]}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 1, // Second compilation is cached,
		},
		{
			note:                "inter-query query cache warmup (default duration)",
			source:              `p := rego.eval({"cache": true, "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			source:              `p := rego.eval({"cache": true, "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
			intraQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (default duration, expired)",
			source:              `p := rego.eval({"cache": true, "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault),
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (explicit duration)",
			source:              `p := rego.eval({"cache": true, "cache_duration": "10s", "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			source:              `p := rego.eval({"cache": true, "cache_duration": "10s", "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
			intraQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (explicit duration, expired)",
			source:              `p := rego.eval({"cache": true, "cache_duration": "10s", "module": "package foo.bar\nx := true", "path": "foo.bar.x"})`,
			result:              `{{"result": {"p": {{"result": true}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10 * time.Second),
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "error w/o raise",
			source:              `p := rego.eval({"module": "package foo.bar\nillegal", "path": "foo"})`,
			result:              "",
			error:               "eval_builtin_error: rego.eval: 1 error occurred: 2:1: rego_parse_error: var cannot be used for rule name",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
		{
			note:                "error with raise",
			source:              `p := rego.eval({"module": "package foo.bar\nillegal", "path": "foo", "raise_error": false})`,
			result:              `{{"result": {"p": {"error": {"message": "1 error occurred: 2:1: rego_parse_error: var cannot be used for rule name"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
			intraQueryCacheHits: 0,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			if !tc.doNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeRego(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits, tc.intraQueryCacheHits)
		})
	}
}

func executeRego(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int, expectedIntraQueryCacheHits int) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModule(module),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}

	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())
	metrics := metrics.New()
	v, err := vm.NewVM().WithExecutable(executable).Eval(ctx, query, vm.EvalOpts{
		Metrics:                metrics,
		Time:                   time,
		Cache:                  builtins.Cache{},
		InterQueryBuiltinCache: interQueryCache,
		StrictBuiltinErrors:    true,
	})
	if expectedError != "" {
		if diff := cmp.Diff(expectedError, err.Error()); diff != "" {
			tb.Fatalf("unexpected error: (-want, +got)\n%s", diff)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}

	if hits := metrics.Counter(rego.RegoEvalInterQueryCacheHits).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}

	if hits := metrics.Counter(rego.RegoEvalIntraQueryCacheHits).Value().(uint64); hits != uint64(expectedIntraQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedIntraQueryCacheHits)
	}
}
