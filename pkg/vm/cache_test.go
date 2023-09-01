package vm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/topdown/cache"
)

func TestEvalCache(t *testing.T) {
	rego := "package test\ncached := input.version"
	query := "test/cached"

	compiler := compile.New().WithTarget(compile.TargetPlan).WithEntrypoints(query).WithBundle(&bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(rego),
				Parsed: ast.MustParseModule(rego),
			},
		},
	})
	if err := compiler.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		t.Fatal(err)
	}

	executable, err := NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		t.Fatal(err)
	}

	hook.OnConfig(context.Background(), &config.Config{
		Extra: map[string]json.RawMessage{
			"eval_cache": json.RawMessage(`{"enabled": true, "input_paths": ["/key"], "ttl": "5s"}`),
		},
	})

	vm := NewVM().WithExecutable(executable)
	now := time.Now()

	var maxSize int64 = 1024 * 1024
	interQueryCache := cache.NewInterQueryCache(&cache.Config{
		InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
			MaxSizeBytes: &maxSize,
		},
	})

	cases := []struct {
		note     string
		input    string
		expected string
		time     time.Time
	}{
		{
			note:     "warmup",
			input:    `{"key": "a", "version": 0}`,
			expected: `{{"result": 0}}`,
			time:     now,
		},
		{
			note:     "cache hit (no input changes for cache key)",
			input:    `{"key": "a", "version": 1}`,
			expected: `{{"result": 0}}`,
			time:     now.Add(time.Second),
		},
		{
			note:     "cache miss (input changes for cache key)",
			input:    `{"key": "b", "version": 1}`,
			expected: `{{"result": 1}}`,
			time:     now.Add(time.Second),
		},
		{
			note:     "cache miss (expired)",
			input:    `{"key": "b", "version": 2}`,
			expected: `{{"result": 2}}`,
			time:     now.Add(6*time.Second + 1), // more than 5s passed since the cache population above.
		},
	}

	for _, tc := range cases {
		t.Run(tc.note, func(t *testing.T) {
			var input interface{}
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}

			_, ctx := WithStatistics(context.Background())
			result, err := vm.Eval(ctx, query, EvalOpts{
				Input:                  &input,
				Time:                   tc.time,
				InterQueryBuiltinCache: interQueryCache,
			})
			if err != nil {
				t.Fatal(err)
			}

			if ast.MustParseTerm(tc.expected).Value.Compare(result) != 0 {
				t.Fatalf("unexpected value: %v", result)
			}
		})
	}
}
