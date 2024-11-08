package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func LaunderType(x interface{}) *interface{} {
	return &x
}

// Note: Currently only implements tests for the Postgres dialect.
func TestUCASTNodeAsSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Note    string
		Source  UCASTNode
		Dialect string
		Result  string
		Error   string
	}{
		{
			Note:    "Nil argument",
			Source:  UCASTNode{Type: "field", Op: "eq", Field: "name", Value: nil},
			Dialect: "postgres",
			Result:  "",
			Error:   "field expression requires a value",
		},
		{
			Note:    "Laundered nil argument",
			Source:  UCASTNode{Type: "field", Op: "eq", Field: "name", Value: LaunderType(nil)},
			Dialect: "postgres",
			Result:  "WHERE name = NULL",
		},
		{
			Note: "Basic compound expression",
			Source: UCASTNode{Type: "compound", Op: "and", Value: LaunderType([]UCASTNode{
				{Type: "field", Op: "eq", Field: "name", Value: LaunderType("bob")},
				{Type: "field", Op: "gt", Field: "salary", Value: LaunderType(50000)},
			})},
			Dialect: "postgres",
			Result:  "WHERE (name = E'bob' AND salary > 50000)",
		},
		{
			Note: "Basic nested compound expression",
			Source: UCASTNode{Type: "compound", Op: "and", Value: LaunderType([]UCASTNode{
				{Type: "field", Op: "eq", Field: "name", Value: LaunderType("bob")},
				{Type: "field", Op: "gt", Field: "salary", Value: LaunderType(50000)},
				{Type: "compound", Op: "or", Value: LaunderType([]UCASTNode{
					{Type: "field", Op: "eq", Field: "role", Value: LaunderType("admin")},
					{Type: "field", Op: "ge", Field: "salary", Value: LaunderType(100000)},
				})},
			})},
			Dialect: "postgres",
			Result:  "WHERE (name = E'bob' AND salary > 50000 AND (role = E'admin' OR salary >= 100000))",
		},
	}

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			t.Parallel()

			cond := sqlbuilder.NewCond()
			where := sqlbuilder.NewWhereClause()
			conditionStr, err := tc.Source.AsSQL(cond, tc.Dialect)
			if err != nil && tc.Error != err.Error() {
				t.Fatal(err)
			}
			where.AddWhereExpr(cond.Args, conditionStr)
			s, args := where.BuildWithFlavor(sqlbuilder.PostgreSQL)

			actual, err := interpolateByDialect(tc.Dialect, s, args)
			if err != nil {
				t.Fatal(err)
			}

			if actual != tc.Result {
				t.Fatalf("expected SQL string: '%s', got string: '%s'", tc.Result, actual)
			}
		})
	}
}

func TestUCASTAsSQLBuiltin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Note    string
		Source  string
		Dialect string
		Result  string
		Error   string
	}{
		{
			Note:    "null argument",
			Source:  `p := ucast.as_sql({"type": "field", "operator": "eq", "field": "name", "value": null}, "postgres", {})`,
			Dialect: "postgres",
			Result:  `{{"result": {"p": "WHERE name = NULL"} }}`,
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

			executeUCASTAsSQLTest(t, "package t\n"+tc.Source, "t", tc.Result, tc.Error, time.Now())
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
