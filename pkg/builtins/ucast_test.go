package builtins

import (
	"testing"

	"github.com/huandu/go-sqlbuilder"
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
			cond := sqlbuilder.NewCond()
			where := sqlbuilder.NewWhereClause()
			where.AddWhereExpr(cond.Args, tc.Source.AsSQL(cond, tc.Dialect))
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
