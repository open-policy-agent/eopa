package ucast

import (
	"testing"
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
			Note:    "startswith + pattern",
			Source:  UCASTNode{Type: "field", Field: "name", Op: "startswith", Value: LaunderType(`f\oo_b%ar`)},
			Dialect: "postgres",
			Result:  `WHERE name LIKE E'f\\\\oo\\_b\\%ar%'`,
		},
		{
			Note:    "endswith + pattern",
			Source:  UCASTNode{Type: "field", Field: "name", Op: "endswith", Value: LaunderType(`f\oo_b%ar`)},
			Dialect: "postgres",
			Result:  `WHERE name LIKE E'%f\\\\oo\\_b\\%ar'`,
		},
		{
			Note:    "contains + pattern",
			Source:  UCASTNode{Type: "field", Field: "name", Op: "contains", Value: LaunderType(`f\oo_b%ar`)},
			Dialect: "postgres",
			Result:  `WHERE name LIKE E'%f\\\\oo\\_b\\%ar%'`,
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
		{
			Note:    "'in' expression",
			Source:  UCASTNode{Type: "field", Field: "f", Op: "in", Value: LaunderType([]any{"foo", "bar"})},
			Dialect: "postgres",
			Result:  "WHERE f IN (E'foo', E'bar')",
		},
		{
			Note: "'not' compound expression",
			Source: UCASTNode{Type: "compound", Op: "not", Value: LaunderType([]UCASTNode{
				{Type: "field", Op: "eq", Field: "name", Value: LaunderType("bob")},
			})},
			Dialect: "postgres",
			Result:  "WHERE NOT name = E'bob'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			t.Parallel()

			actual, err := tc.Source.AsSQL(tc.Dialect)
			if err != nil && tc.Error != err.Error() {
				t.Fatal(err)
			}

			if actual != tc.Result {
				t.Fatalf("expected SQL string: '%s', got string: '%s'", tc.Result, actual)
			}
		})
	}
}
