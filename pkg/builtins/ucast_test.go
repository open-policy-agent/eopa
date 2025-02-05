package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestUCASTAsSQLBuiltin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Note    string
		Source  string
		Dialect string
		Result  string
		Error   string
		Query   string
	}{
		{
			Note:    "null argument, eq",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "eq", "field": "name", "value": null}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name IS NULL"} }}`,
		},
		{
			Note:    "null argument, ne",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "ne", "field": "name", "value": null}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name IS NOT NULL"} }}`,
		},
		{
			Note:    "field ref argument",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "eq", "field": "name", "value": {"field": "x"}}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name = x"} }}`,
		},
		{
			Note:    "field ref argument with translations",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "eq", "field": "tbl.col", "value": {"field": "tbl.col2"}}, "postgres", {"tbl": {"$self": "table", "col": "column", "col2": "column2"}})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE table.column = table.column2"} }}`,
		},
		{
			Note: "basic compound expression",
			Source: `p := ucast.as_sql({"type": "compound", "operator": "and", "value": [
				{"type": "field", "operator": "eq", "field": "name", "value": "bob"},
				{"type": "field", "operator": "gt", "field": "salary", "value": 50000},
			]}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE (name = E'bob' AND salary > 50000)"} }}`,
		},
		{
			Note: "basic compound expression built via multi-value rule",
			Source: `
a contains {"type": "field", "operator": "eq", "field": "name", "value": "bob"}
a contains {"type": "field", "operator": "gt", "field": "salary", "value": 50000}
p := ucast.as_sql({"type": "compound", "operator": "and", "value": a}, "postgres", {})`,
			Dialect: "postgres",
			Query:   "t/p",
			Result:  `{{"result": "WHERE (name = E'bob' AND salary > 50000)"}}`,
		},
		{
			Note: "basic nested compound expression",
			Source: `p := ucast.as_sql({"type": "compound", "operator": "and", "value": [
				{"type": "field", "operator": "eq", "field": "name", "value": "bob"},
				{"type": "field", "operator": "gt", "field": "salary", "value": 50000},
				{"type": "compound", "operator": "or", "value": [
					{"type": "field", "operator": "eq", "field": "role", "value": "admin"},
					{"type": "field", "operator": "ge", "field": "salary", "value": 100000},
				]},
			]}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE (name = E'bob' AND salary > 50000 AND (role = E'admin' OR salary >= 100000))"} }}`,
		},
		{
			Note: "nested compound expression with table and column name translations",
			Source: `p := ucast.as_sql({"type": "compound", "operator": "and", "value": [
				{"type": "field", "operator": "eq", "field": "users.name", "value": "bob"},
				{"type": "field", "operator": "gt", "field": "finance.salary", "value": 50000},
				{"type": "compound", "operator": "or", "value": [
					{"type": "field", "operator": "eq", "field": "users.role", "value": "admin"},
					{"type": "field", "operator": "ge", "field": "finance.salary", "value": 100000},
				]},
			]}, "postgres", {"users": {"$self": "users0", "name": "name0", "role": "role0"}, "finance": {"salary": "salary0"}})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE (users0.name0 = E'bob' AND finance.salary0 > 50000 AND (users0.role0 = E'admin' OR finance.salary0 >= 100000))"} }}`,
		},
		{
			Note:    "malformed field expression",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "or", "field": "name", "value": []}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name = NULL"} }}`,
			Error:   "eval_builtin_error: ucast.as_sql: unrecognized operator: or",
		},
		{
			Note:    "malformed compound expression",
			Source:  `p := ucast.as_sql({"type": "compound", "operator": "and", "value": "AAA"}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name = NULL"} }}`,
			Error:   "eval_builtin_error: ucast.as_sql: value must be an array",
		},
	}

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			t.Parallel()
			query := "t"
			if q := tc.Query; q != "" {
				query = q
			}

			executeUCASTAsSQLTest(t, "package t\n"+tc.Source, query, tc.Result, tc.Error, time.Now())
		})
	}
}

func executeUCASTAsSQLTest(tb testing.TB, module string, query string, expectedResult string, expectedError string, time time.Time) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModuleWithOpts(module, ast.ParserOptions{RegoVersion: ast.RegoV1}),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query).WithRegoVersion(ast.RegoV1)
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
		Metrics:             metrics,
		Time:                time,
		Cache:               builtins.Cache{},
		StrictBuiltinErrors: true,
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
}

func TestExpand(t *testing.T) {
	for _, tc := range []struct {
		note     string
		input    string
		expected string
		err      error
	}{
		{
			note:     "simple eq",
			input:    `{"users.name": "alice"}`,
			expected: `{"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}`,
		},
		{
			note:     "explicit eq",
			input:    `{"users.name": {"eq": "alice"}}`,
			expected: `{"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}`,
		},
		{
			note:     "simple op",
			input:    `{"users.age": {"gt": 20}}`,
			expected: `{"type": "field", "operator": "gt", "field": "users.age", "value": 20}`,
		},
		{
			note:  "simple op, multiple field conditions",
			input: `{"users.age": {"gt": 20, "lt": 100}}`,
			err:   errMultipleFieldConditions,
		},
		{
			note:  "compound and",
			input: `{"users.age": {"gt": 20}, "users.name": "alice"}`,
			expected: `{"type": "compound", "operator": "and", "value": [
						{"type": "field", "operator": "gt", "field": "users.age", "value": 20},
						{"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}]}`,
		},
		{
			note:  "compound or",
			input: `{"or": [{"users.age": {"gt": 20}}, {"users.name": "alice"}]}`,
			expected: `{"type": "compound", "operator": "or", "value": [
						{"type": "field", "operator": "gt", "field": "users.age", "value": 20},
						{"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}]}`,
		},
		{
			note:  "compound or, one disjunct expanded already",
			input: `{"or": [{"users.age": {"gt": 20}}, {"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}]}`,
			expected: `{"type": "compound", "operator": "or", "value": [
						{"type": "field", "operator": "gt", "field": "users.age", "value": 20},
						{"type": "field", "operator": "eq", "field": "users.name", "value": "alice"}]}`,
		},
		{
			// NOTE(sr): this would happen for multi-value rules when
			// none of the "or contains ..." rules yield anything.
			note:     "compound or, empty set",
			input:    `{"or": set()}`,
			expected: `{}`,
		},
		{
			note:  "compound or, bad value type",
			input: `{"or": {"users.age": {"gt": 20}}}`,
			err:   errBadOrValue,
		},
		{
			note:  "compound or, with nested and",
			input: `{"or": [{"users.age": {"gt": 20}, "users.name": "bob"}, {"users.name": "alice"}]}`,
			expected: `{"type": "compound", "operator": "or", "value": [
						{"operator": "and", "type": "compound", "value": [
							{"field": "users.age", "operator": "gt", "type": "field", "value": 20},
							{"field": "users.name", "operator": "eq", "type": "field", "value": "bob"}]},
						{"field": "users.name", "operator": "eq", "type": "field", "value": "alice"}]}`,
		},
		{
			note: "compound or, with nested and, already-expanded",
			input: `{"or": [{"operator": "and", "type": "compound", "value": [
							  {"field": "users.age", "operator": "gt", "type": "field", "value": 20},
							  {"field": "users.name", "operator": "eq", "type": "field", "value": "bob"}]},
							{"users.name": "alice"}]}`,
			expected: `{"type": "compound", "operator": "or", "value": [
						{"operator": "and", "type": "compound", "value": [
							{"field": "users.age", "operator": "gt", "type": "field", "value": 20},
							{"field": "users.name", "operator": "eq", "type": "field", "value": "bob"}]},
						{"field": "users.name", "operator": "eq", "type": "field", "value": "alice"}]}`,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()
			inp := ast.MustParseTerm(tc.input)
			if tc.err == nil {
				exp := ast.MustParseTerm(tc.expected)
				result, _ := expand(inp)
				if !result.Equal(exp) {
					t.Fatalf("Expected %v but got %v", tc.expected, result)
				}
				t.Run("second", func(t *testing.T) { // check idempotency
					result2, _ := expand(result)
					if !result2.Equal(exp) {
						t.Fatalf("Second run: expected %v but got %v", tc.expected, result2)
					}
				})
			} else {
				_, err := expand(inp)
				if !errors.Is(err, tc.err) {
					t.Fatalf("expected error %v, got %v", tc.err, err)
				}
			}
		})
	}
}
