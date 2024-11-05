package builtins

import (
	"encoding/json"
	"slices"

	"github.com/huandu/go-sqlbuilder"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

const (
	ucastAsSQLName = "ucast.as_sql"
)

var (
	compoundOps = []string{"and", "or", "not"}
	documentOps = []string{"exists"}
	fieldOps    = []string{"eq", "ne", "gt", "lt", "ge", "le", "gte", "lte"} //"in", "nin"

	ucastAsSQL = &ast.Builtin{
		Name:        ucastAsSQLName,
		Description: "Translates a UCAST conditions AST into an SQL WHERE clause of the given dialect.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("ucast conditions object"),
			),
			types.Named("response", types.NewString()).Description("dialect"),
		),
		// Categories: docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/ucast"),
	}
)

type UCASTCondition interface {
	AsSQL(dialect string) string
}

// The "union" structure for incoming UCAST trees.
type UCASTNode struct {
	Type  string       `json:"type"`
	Op    string       `json:"operator"`
	Field string       `json:"field,omitempty"`
	Value *interface{} `json:"value,omitempty"`
}

func dialectToFlavor(dialect string) sqlbuilder.Flavor {
	switch dialect {
	case "mysql":
		return sqlbuilder.MySQL
	case "sqlite":
		return sqlbuilder.SQLite
	case "postgres":
		return sqlbuilder.PostgreSQL
	default:
		return sqlbuilder.SQLite
	}
}

func interpolateByDialect(dialect string, s string, args []interface{}) (string, error) {
	switch dialect {
	case "mysql":
		return sqlbuilder.MySQL.Interpolate(s, args)
	case "sqlite":
		return sqlbuilder.SQLite.Interpolate(s, args)
	case "postgres":
		return sqlbuilder.PostgreSQL.Interpolate(s, args)
	default:
		return sqlbuilder.SQLite.Interpolate(s, args)
	}
}

// Uses our SQL generator library to build up a larger SQL expression.
func (u *UCASTNode) AsSQL(cond *sqlbuilder.Cond, dialect string) string {
	cond.Args.Flavor = dialectToFlavor(dialect)
	uType := u.Type
	operator := u.Op
	field := u.Field
	value := u.Value

	switch {
	case slices.Contains(fieldOps, operator) || uType == "field":
		// Note: We should add unary operations under this case, like NOT.
		if value == nil {
			return ""
		}
		switch operator {
		case "eq":
			return cond.Equal(field, *value)
		case "ne":
			return cond.NotEqual(field, *value)
		case "gt":
			return cond.GreaterThan(field, *value)
		case "lt":
			return cond.LessThan(field, *value)
		case "ge", "gte":
			return cond.GreaterEqualThan(field, *value)
		case "le", "lte":
			return cond.LessEqualThan(field, *value)
		}
	case slices.Contains(documentOps, operator) || uType == "document":
		// Note: We should add unary operations under this case, like NOT.
		if value == nil {
			return ""
		}
		if operator == "exists" {
			return cond.Exists(*value)
		}
	case slices.Contains(compoundOps, operator) || uType == "compound":
		switch operator {
		case "and":
			if value == nil {
				return ""
			}
			if values, ok := (*value).([]UCASTNode); ok {
				conds := make([]string, 0, len(values))
				for _, c := range values {
					conds = append(conds, c.AsSQL(cond, dialect))
				}
				return cond.And(conds...)
			}
		case "or":
			if value == nil {
				return ""
			}
			if values, ok := (*value).([]UCASTNode); ok {
				conds := make([]string, 0, len(values))
				for _, c := range values {
					conds = append(conds, c.AsSQL(cond, dialect))
				}
				return cond.Or(conds...)
			}
		}
	}
	return ""
}

// Renders a ucast conditional tree as SQL for a given SQL dialect.
func builtinUcastAsSQL(_ topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	obj, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}
	dialect, err := builtins.StringOperand(operands[1].Value, 2)
	if err != nil {
		return err
	}

	// Round-trip through JSON to extract something we can interpret over.
	var conds UCASTNode
	bs, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if err := util.Unmarshal(bs, &conds); err != nil {
		return err
	}

	// Build up the SQL expression using the UCASTNode tree.
	cond := sqlbuilder.NewCond()
	where := sqlbuilder.NewWhereClause()
	where.AddWhereExpr(cond.Args, conds.AsSQL(cond, string(dialect)))
	s, args := where.BuildWithFlavor(dialectToFlavor(string(dialect)))

	// Interpolate in the arguments into the SQL string.
	interpolatedQuery, err := interpolateByDialect(string(dialect), s, args)
	if err != nil {
		return err
	}

	return iter(ast.StringTerm(interpolatedQuery))
}

func init() {
	RegisterBuiltinFunc(ucastAsSQLName, builtinUcastAsSQL)
}
