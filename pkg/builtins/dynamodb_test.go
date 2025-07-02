//go:build !race

package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestDynamoDBGet(t *testing.T) {
	t.Parallel()

	ddb, endpoint := startDynamoDB(t)
	defer ddb.Terminate(context.Background())

	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
	}{
		{
			note:                "missing parameter(s)",
			source:              `p = resp if { dynamodb.get({}, resp)}`,
			result:              "",
			error:               `eval_type_error: dynamodb.get: operand 1 missing required request parameter(s): {"key", "region", "table"}`,
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "non-existing row query",
			source:              fmt.Sprintf(`p := dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "nonexisting"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "a single row query",
			source:              fmt.Sprintf(`p := dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note: "intra-query query cache",
			source: fmt.Sprintf(`p = [ resp1, resp2 ] if {
								                                dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}, resp1)
								                                dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}, resp2) # cached
												}`, endpoint, endpoint),
			result:              `{{"result": {"p": [{"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}, {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}]}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (default duration)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (default duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault),
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (explicit duration)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (explicit duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.get({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10 * time.Second),
			interQueryCacheHits: 0,
		},
		{
			note:                "error w/o raise",
			source:              fmt.Sprintf(`p := dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}})`, endpoint),
			result:              "",
			error:               "eval_builtin_error: dynamodb.get: ValidationException: One or more parameter values were invalid: Type mismatch for key",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "error with raise",
			source:              fmt.Sprintf(`p := dynamodb.get({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}, "raise_error": false})`, endpoint),
			result:              `{{"result": {"p": {"error": {"code": "ValidationException", "message": "One or more parameter values were invalid: Type mismatch for key"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			if !tc.doNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeDynamoDB(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, dynamoDBGetInterQueryCacheHits, tc.interQueryCacheHits)
		})
	}
}

func TestDynamoDBQuery(t *testing.T) {
	t.Parallel()

	ddb, endpoint := startDynamoDB(t)
	defer ddb.Terminate(context.Background())

	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
	}{
		{
			note:                "missing parameter(s)",
			source:              `p = resp if { dynamodb.query({}, resp)}`,
			result:              "",
			error:               `eval_type_error: dynamodb.query: operand 1 missing required request parameter(s): {"key_condition_expression", "region", "table"}`,
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "a multi row query",
			source:              fmt.Sprintf(`p := dynamodb.query({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"},{"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note: "intra-query query cache",
			source: fmt.Sprintf(`p = [ resp1, resp2 ] if {
								                                dynamodb.query({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}}, resp1)
								                                dynamodb.query({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}}, resp2) # cached
												}`, endpoint, endpoint),
			result:              `{{"result": {"p": [{"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}, {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"},{"number": 4321, "p": "x", "s": 2, "string": "value2"}]}]}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (default duration)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (default duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault),
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (explicit duration)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (explicit duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.query({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"}, {"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second + 1),
			interQueryCacheHits: 0,
		},
		{
			note:                "error w/o raise",
			source:              fmt.Sprintf(`p := dynamodb.query({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"N": "0"}}, "expression_attribute_names": {"#p": "p"}})`, endpoint),
			result:              "",
			error:               "eval_builtin_error: dynamodb.query: ValidationException: One or more parameter values were invalid: Condition parameter type does not match schema type",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "error with raise",
			source:              fmt.Sprintf(`p := dynamodb.query({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"N": "0"}}, "expression_attribute_names": {"#p": "p"}, "raise_error": false})`, endpoint),
			result:              `{{"result": {"p": {"error": {"code": "ValidationException", "message": "One or more parameter values were invalid: Condition parameter type does not match schema type"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			if !tc.doNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeDynamoDB(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, dynamoDBQueryInterQueryCacheHits, tc.interQueryCacheHits)
		})
	}
}

func executeDynamoDB(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, interQueryCacheHitsKey string, expectedInterQueryCacheHits int) {
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
		if !strings.HasPrefix(err.Error(), expectedError) {
			tb.Fatalf("unexpected error: %v", err)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}

	if hits := metrics.Counter(interQueryCacheHitsKey).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}
