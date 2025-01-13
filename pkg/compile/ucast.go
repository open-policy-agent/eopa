package compile

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"

	ucast "github.com/styrainc/enterprise-opa-private/pkg/builtins"
)

type Opts struct {
	Translations map[string]any
}

func BodiesToUCAST(bs []ast.Body, opts *Opts) *ucast.UCASTNode {
	if len(bs) == 0 {
		return nil
	}

	// If there's only one body, convert it directly
	if len(bs) == 1 {
		return bodyToUCAST(bs[0], opts)
	}

	// Multiple expressions are combined with AND
	nodes := make([]ucast.UCASTNode, len(bs))
	for i := range bs {
		u := bodyToUCAST(bs[i], opts)
		if u == nil {
			return nil
		}
		nodes[i] = *u
	}
	value := any(nodes)
	return &ucast.UCASTNode{
		Type:  "compound",
		Op:    "or",
		Value: &value,
	}
}

func bodyToUCAST(body ast.Body, opts *Opts) *ucast.UCASTNode {
	if len(body) == 0 {
		return nil
	}

	// If there's only one expression, convert it directly
	if len(body) == 1 {
		return exprToUCAST(body[0], opts)
	}

	// Multiple expressions are combined with AND
	nodes := make([]ucast.UCASTNode, len(body))
	for i, expr := range body {
		u := exprToUCAST(expr, opts)
		if u == nil {
			return nil
		}
		nodes[i] = *u
	}
	value := any(nodes)
	return &ucast.UCASTNode{
		Type:  "compound",
		Op:    "and",
		Value: &value,
	}
}

func exprToUCAST(expr *ast.Expr, opts *Opts) *ucast.UCASTNode {
	if expr == nil || !expr.IsCall() {
		return nil
	}

	ref, flip := refFromCall(expr)
	return callToNode(expr, ref, flip, opts)
}

// refToField drops the first part of ast.Ref, and joins the rest with "."
func refToField(r ast.Ref, opts *Opts) (string, error) {
	parts := make([]string, len(r)-1)
	for i := range len(r) - 1 {
		switch t := r[i+1].Value.(type) {
		case ast.Var:
			parts[i] = string(t)
		case ast.String:
			parts[i] = string(t)
		default:
			return "", fmt.Errorf("unexpected type in ref %v: %T (%[2]v)", r, t)
		}
	}
	return translateField(strings.Join(parts, "."), opts.Translations), nil
}

func toFieldNode(op string, r ast.Ref, v ast.Value, opts *Opts) *ucast.UCASTNode {
	value, err := ast.ValueToInterface(v, nil)
	if err != nil {
		return nil
	}
	f, err := refToField(r, opts)
	if err != nil {
		return nil
	}
	return &ucast.UCASTNode{
		Type:  "field",
		Op:    op,
		Field: f,
		Value: &value,
	}
}

var reversed = map[string]string{
	"lt":  "gte",
	"lte": "gt",
	"gt":  "lte",
	"gte": "lt",
}

// callToNode converts a call expression to a UCASTNode, and flips the arguments
// and the comparison operator if needed.
func callToNode(e *ast.Expr, f ast.Ref, flip bool, opts *Opts) *ucast.UCASTNode {
	ref := e.OperatorTerm().Value.(ast.Ref)
	op := ref.String()

	switch {
	case ref.Equal(ast.Equality.Ref()):
	case ref.Equal(ast.NotEqual.Ref()):
		op = "ne"
	case ref.Equal(ast.LessThan.Ref()):
	case ref.Equal(ast.LessThanEq.Ref()):
	case ref.Equal(ast.GreaterThan.Ref()):
	case ref.Equal(ast.GreaterThanEq.Ref()):
	case ref.Equal(ast.StartsWith.Ref()):
	case ref.Equal(ast.EndsWith.Ref()):
	case ref.Equal(ast.Contains.Ref()):
	case ref.Equal(ast.Member.Ref()):
		op = "in"
	default:
		return nil
	}

	i := 1
	if flip {
		i = 0
		op = cmp.Or(reversed[op], op) // optionally replace operator
	}

	return toFieldNode(op, f, e.Operand(i).Value, opts)
}

func refFromCall(e *ast.Expr) (ast.Ref, bool) {
	leftRef, ok := e.Operand(0).Value.(ast.Ref)
	if ok { // lhs is unknown
		return leftRef, false
	}
	return e.Operand(1).Value.(ast.Ref), true // rhs is unknown
}

func translateField(field string, translations map[string]any) string {
	var outTable, outColumn string
	if translations == nil {
		return field
	}
	before, after, found := strings.Cut(field, ".")
	outTable = before
	outColumn = after
	// Is there a translation available for the table name?
	if tableMapping, ok := translations[before]; ok {
		if tableMapping, ok := tableMapping.(map[string]any); ok {
			// See if there's a mapping for the table name, and remap.
			if tableName, ok := tableMapping["$self"]; ok {
				outTable = tableName.(string) // XXX: be more cautious about the type
			}
			// If we have a column name, try remapping it.
			if found {
				if columnName, ok := tableMapping[after]; ok {
					outColumn = columnName.(string)
				}
			}
		}
	}
	if found {
		return outTable + "." + outColumn
	}
	return outTable
}
