package builtins

import (
	"fmt"
	"slices"

	"github.com/huandu/go-sqlbuilder"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
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
				types.Named("conditions", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("ucast conditions object"),
				types.Named("dialect", types.NewString()).Description("dialect"),
			),
			types.Named("result", types.NewString()).Description("generated sql"),
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
func (u *UCASTNode) AsSQL(cond *sqlbuilder.Cond, dialect string) (string, error) {
	cond.Args.Flavor = dialectToFlavor(dialect)
	uType := u.Type
	operator := u.Op
	field := u.Field
	value := u.Value

	switch {
	case slices.Contains(fieldOps, operator) || uType == "field":
		// Note: We should add unary operations under this case, like NOT.
		if value == nil {
			return "", fmt.Errorf("field expression requires a value")
		}
		switch operator {
		case "eq":
			return cond.Equal(field, *value), nil
		case "ne":
			return cond.NotEqual(field, *value), nil
		case "gt":
			return cond.GreaterThan(field, *value), nil
		case "lt":
			return cond.LessThan(field, *value), nil
		case "ge", "gte":
			return cond.GreaterEqualThan(field, *value), nil
		case "le", "lte":
			return cond.LessEqualThan(field, *value), nil
		default:
			return "", fmt.Errorf("unrecognized operator: %s", operator)
		}
	case slices.Contains(documentOps, operator) || uType == "document":
		// Note: We should add unary operations under this case, like NOT.
		if value == nil {
			return "", fmt.Errorf("document expression 'exists' requires a value")
		}
		if operator == "exists" {
			return cond.Exists(*value), nil
		}
		return "", fmt.Errorf("unrecognized operator: %s", operator)
	case slices.Contains(compoundOps, operator) || uType == "compound":
		switch operator {
		case "and":
			if value == nil {
				return "", fmt.Errorf("compound expression 'and' requires a value")
			}
			if values, ok := (*value).([]UCASTNode); ok {
				conds := make([]string, 0, len(values))
				for _, c := range values {
					condition, err := c.AsSQL(cond, dialect)
					if err != nil {
						return "", err
					}
					conds = append(conds, condition)
				}
				return cond.And(conds...), nil
			}
			return "", fmt.Errorf("value must be an array")
		case "or":
			if value == nil {
				return "", fmt.Errorf("compound expression 'or' requires a value")
			}
			if values, ok := (*value).([]UCASTNode); ok {
				conds := make([]string, 0, len(values))
				for _, c := range values {
					condition, err := c.AsSQL(cond, dialect)
					if err != nil {
						return "", err
					}
					conds = append(conds, condition)
				}
				return cond.Or(conds...), nil
			}
			return "", fmt.Errorf("value must be an array")
		}
		return "", fmt.Errorf("unrecognized operator: %s", operator)
	default:
		return "", fmt.Errorf("unrecognized operator: %s", operator)
	}
}

func launderType(x interface{}) *interface{} {
	return &x
}

// This method recursively traverses the object provided, and attempts to
// construct a UCASTNode tree from it.
func regoObjectToUCASTNode(obj *ast.Term) (*UCASTNode, error) {
	out := &UCASTNode{}

	ty := obj.Get(ast.StringTerm("type"))
	op := obj.Get(ast.StringTerm("operator"))
	field := obj.Get(ast.StringTerm("field"))
	value := obj.Get(ast.StringTerm("value"))

	if ty == nil || op == nil {
		return nil, builtins.NewOperandErr(1, "ucast.as_sql", "type and operator fields are required")
	}
	out.Type = string(ty.Value.(ast.String))
	out.Op = string(op.Value.(ast.String))

	if field != nil {
		out.Field = string(field.Value.(ast.String))
	}

	// Change handling, based on type. If we get an array, recurse.
	// Numeric types also have to be converted from json.Number to int/float.
	if value != nil {
		switch x := value.Value.(type) {
		case *ast.Array:
			var iterErr error
			nodes := make([]UCASTNode, 0, x.Len())
			x.Foreach(func(elem *ast.Term) {
				node, err := regoObjectToUCASTNode(elem)
				if err != nil {
					iterErr = err
				}
				nodes = append(nodes, *node)
			})
			if iterErr != nil {
				return nil, iterErr
			}
			out.Value = launderType(nodes)
		case ast.Number:
			if intNum, ok := x.Int64(); ok {
				out.Value = launderType(intNum)
			} else if floatNum, ok := x.Float64(); ok {
				out.Value = launderType(floatNum)
			} else {
				out.Value = launderType(x)
			}
		default:
			valueIf, err := ast.JSON(value.Value)
			if err != nil {
				return nil, err
			}
			out.Value = launderType(valueIf)
		}
	}

	return out, nil
}

// Renders a ucast conditional tree as SQL for a given SQL dialect.
func builtinUcastAsSQL(_ topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}
	dialect, err := builtins.StringOperand(operands[1].Value, 2)
	if err != nil {
		return err
	}

	// Round-trip through JSON to extract something we can interpret over.
	conds, err := regoObjectToUCASTNode(operands[0])
	if err != nil {
		return err
	}

	// Build up the SQL expression using the UCASTNode tree.
	cond := sqlbuilder.NewCond()
	where := sqlbuilder.NewWhereClause()
	conditionStr, err := conds.AsSQL(cond, string(dialect))
	if err != nil {
		return err
	}
	where.AddWhereExpr(cond.Args, conditionStr)
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
