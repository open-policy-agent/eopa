package builtins

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/huandu/go-sqlbuilder"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
)

const (
	ucastAsSQLName  = "ucast.as_sql"
	ucastExpandName = "ucast.expand"
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
				types.Named("translations", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("table and column name translations"),
			),
			types.Named("result", types.NewString()).Description("generated sql"),
		),
		// Categories: docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/ucast"),
	}

	ucastExpand = &ast.Builtin{
		Name:        ucastExpandName,
		Description: "Expands the concise UCAST AST into its normalized form.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("conditions", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("ucast conditions object"),
			),
			types.Named("result", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("expanded"),
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

// Handles splitting an SQL table/column field name into its component pieces,
// and then translates the parts over appropriately when possible.
func translateField(field string, translations *ast.Term) string {
	var outTable, outColumn string
	if translations == nil {
		return field
	}
	before, after, found := strings.Cut(field, ".")
	outTable = before
	outColumn = after
	// Is there a translation available for the table name?
	if tableMapping := translations.Get(ast.StringTerm(before)); tableMapping != nil {
		if _, ok := tableMapping.Value.(ast.Object); ok {
			// See if there's a mapping for the table name, and remap.
			if tableName := tableMapping.Get(ast.StringTerm("$self")); tableName != nil {
				outTable = string(tableName.Value.(ast.String))
			}
			// If we have a column name, try remapping it.
			if found {
				if columnName := tableMapping.Get(ast.StringTerm(after)); columnName != nil {
					outColumn = string(columnName.Value.(ast.String))
				}
			}
		}
	}
	if found {
		return outTable + "." + outColumn
	}
	return outTable
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
func regoObjectToUCASTNode(obj *ast.Term, translations *ast.Term) (*UCASTNode, error) {
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
		out.Field = translateField(string(field.Value.(ast.String)), translations)
	}

	// Change handling, based on type. If we get an array, recurse.
	// Numeric types also have to be converted from json.Number to int/float.
	if value != nil {
		switch x := value.Value.(type) {
		case interface {
			Iter(func(*ast.Term) error) error
			Len() int
		}:
			nodes := make([]UCASTNode, 0, x.Len())
			if err := x.Iter(func(elem *ast.Term) error {
				node, err := regoObjectToUCASTNode(elem, translations)
				if err != nil {
					return err
				}
				nodes = append(nodes, *node)
				return nil
			}); err != nil {
				return nil, err
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
	obj := operands[0]
	translations := operands[2]
	_, err := builtins.ObjectOperand(obj.Value, 1)
	if err != nil {
		return err
	}
	dialect, err := builtins.StringOperand(operands[1].Value, 2)
	if err != nil {
		return err
	}
	_, err = builtins.ObjectOperand(translations.Value, 3)
	if err != nil {
		return err
	}

	// Round-trip through JSON to extract something we can interpret over.
	conds, err := regoObjectToUCASTNode(obj, translations)
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

func builtinUcastExpand(_ topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	obj := operands[0]
	exp, err := expand(obj)
	if err != nil {
		return err
	}
	return iter(exp)
}

func init() {
	RegisterBuiltinFunc(ucastAsSQLName, builtinUcastAsSQL)
	RegisterBuiltinFunc(ucastExpandName, builtinUcastExpand)
}

var errBadInput = errors.New("malformed conditions object")
var errBadOrValue = fmt.Errorf("%w: 'or' value must be an array/set", errBadInput)
var errMultipleFieldConditions = fmt.Errorf("%w: multiple field conditions", errBadInput)

func expand(in *ast.Term) (*ast.Term, error) {
	if o, ok := in.Value.(ast.Object); ok {
		return expandObject(o)
	}
	return nil, errBadInput
}

func expandObject(in ast.Object) (*ast.Term, error) {
	switch in.Len() {
	case 0:
		return nil, nil // TODO(sr): think about this
	case 1:
		k := in.Keys()[0]
		switch k.Value.(ast.String) {
		case "or":
			return compoundOr(in.Get(k))
		default:
			return fieldOpFromVal(k, in.Get(k).Value)
		}
	default:
		return compoundAnd(in)
	}
}

func compoundOr(in *ast.Term) (*ast.Term, error) {
	if in, ok := in.Value.(interface { // ast.Set or *ast.Array
		Iter(func(*ast.Term) error) error
		Len() int
	}); ok {
		if in.Len() == 0 { // no "or" conditions, drop the entire condition
			return ast.ObjectTerm(), nil
		}
		vals := make([]*ast.Term, 0, in.Len())
		if err := in.Iter(func(val *ast.Term) error {
			if t, err := expandObject(val.Value.(ast.Object)); err != nil {
				return err
			} else {
				vals = append(vals, t)
			}
			return nil
		}); err != nil {
			return nil, err
		}
		return ast.ObjectTerm(
			[2]*ast.Term{opTerm, orTerm},
			[2]*ast.Term{typeTerm, compoundType},
			[2]*ast.Term{valueTerm, ast.ArrayTerm(vals...)},
		), nil
	}

	return nil, errBadOrValue
}

func compoundAnd(in ast.Object) (*ast.Term, error) {
	vals := make([]*ast.Term, 0, in.Len())
	if err := in.Iter(func(key, val *ast.Term) error {
		if t, err := fieldOpFromVal(key, val.Value); err != nil {
			return err
		} else {
			vals = append(vals, t)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return ast.ObjectTerm(
		[2]*ast.Term{opTerm, andTerm},
		[2]*ast.Term{typeTerm, compoundType},
		[2]*ast.Term{valueTerm, ast.ArrayTerm(vals...)},
	), nil
}

func fieldOpFromVal(field *ast.Term, value ast.Value) (*ast.Term, error) {
	switch value := value.(type) {
	case ast.Object:
		switch value.Len() {
		case 1:
			op := value.Keys()[0]
			return fieldOp(string(op.Value.(ast.String)), field, value.Get(op).Value)
		default:
			return nil, errMultipleFieldConditions
		}
	default: // "eq"
		return fieldOp("eq", field, value)
	}
}

func fieldOp(op string, field *ast.Term, value ast.Value) (*ast.Term, error) {
	return ast.ObjectTerm(
		[2]*ast.Term{opTerm, ast.StringTerm(op)},
		[2]*ast.Term{typeTerm, fieldType},
		[2]*ast.Term{fieldTerm, field},
		[2]*ast.Term{valueTerm, ast.NewTerm(value)},
	), nil
}

var (
	opTerm    = ast.StringTerm("operator")
	typeTerm  = ast.StringTerm("type")
	fieldTerm = ast.StringTerm("field")
	valueTerm = ast.StringTerm("value")
	andTerm   = ast.StringTerm("and")
	orTerm    = ast.StringTerm("or")
)

var (
	fieldType    = ast.StringTerm("field")
	compoundType = ast.StringTerm("compound")
)
