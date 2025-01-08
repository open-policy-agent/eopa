package compile

import (
	"fmt"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
)

const code = "pe_fragment_error" // TODO(sr): this is preliminary

type Results struct {
	errs []*ast.Error
}

func (r *Results) ASTErrors() []*ast.Error {
	if r == nil {
		return nil
	}
	return r.errs
}

func Check(pq *rego.PartialQueries) *Results {
	res := Results{}
	for i := range pq.Queries {
		checkQuery(pq.Queries[i], pq.Support, &res)
	}
	// NOTE(sr): So far, we've gotten better error locations from the refs into
	// support modules. The support modules themselves are surprisingly useless
	// for that.
	// for i := range pq.Support {
	// 	checkSupport(pq.Support[i], &res)
	// }
	return &res
}

func checkQuery(q ast.Body, sup []*ast.Module, res *Results) {
	for j := range q {
		for i := range queryChecks {
			if err := queryChecks[i](q[j], sup); err != nil {
				res.errs = append(res.errs, err)
			}
		}
	}
}

var queryChecks = [...]func(*ast.Expr, []*ast.Module) *ast.Error{
	checkCall,
	checkBuiltins,
}

var partialPrefix = ast.MustParseRef("data.partial")

func checkCall(e *ast.Expr, sup []*ast.Module) *ast.Error {
	switch {
	case e.Negated:
		return err(e.Loc(), "\"not\" not permitted")
	case e.IsCall(): // OK
	case e.IsEvery():
		return err(e.Loc(), "\"every\" not permitted")
	case len(e.With) > 0:
		return err(e.Loc(), "\"with\" not permitted")
	default:
		if t, ok := e.Terms.(*ast.Term); ok {
			if ref, ok := t.Value.(ast.Ref); ok && ref.HasPrefix(partialPrefix) {
				loc := ref[len(ref)-1].Loc()
				return err(loc, "%s", findDetails(ref, sup))
			}

			if ref, ok := t.Value.(ast.Ref); ok && ref.HasPrefix(ast.DefaultRootRef) {
				// TODO(sr): point to rule with else -- but we don't have the full rego yet
				return withDetails(err(e.Loc(), "invalid data reference \"%v\"", e),
					fmt.Sprintf("has rule \"%v\" an `else`?", ref),
				)
			}
		}
		return withDetails(err(e.Loc(), "invalid statement \"%v\"", e),
			fmt.Sprintf("try `%v != false`", e),
		)
	}
	return nil
}

func checkBuiltins(e *ast.Expr, _ []*ast.Module) *ast.Error {
	if len(e.With) > 0 { // Ignore expression, we'll already have recorded errors through checkCalls.
		return nil
	}
	op := e.OperatorTerm()
	if op == nil {
		return nil
	}
	loc := op.Loc()
	ref := op.Value.(ast.Ref)

	switch {
	case ref.Equal(ast.Equality.Ref()):
	case ref.Equal(ast.NotEqual.Ref()):
	case ref.Equal(ast.LessThan.Ref()):
	case ref.Equal(ast.LessThanEq.Ref()):
	case ref.Equal(ast.GreaterThan.Ref()):
	case ref.Equal(ast.GreaterThanEq.Ref()):
	case ref.Equal(ast.Member.Ref()) || ref.Equal(ast.MemberWithKey.Ref()):
		return err(loc, "invalid use of \"... in ...\"")
	case ref.HasPrefix(ast.DefaultRootRef):
		// TODO(sr): point to function with else -- but we don't have the full rego yet
		return withDetails(err(e.Loc(), "invalid data reference \"%v\"", e),
			fmt.Sprintf("has function \"%v(...)\" an `else`?", ref),
		)

	default:
		return err(loc, "invalid builtin %v", op)
	}

	// all our allowed builtins have two operands
	for i := range 2 {
		if err := checkOperand(op, e.Operand(i)); err != nil {
			return err
		}
	}

	// lhs or rhs needs to be ground scalar
	// TODO(sr): collections might work, too, let's fix this later
	found := false
	for i := range 2 {
		if ast.IsScalar(e.Operand(i).Value) {
			found = true
		}
	}
	if !found {
		if loc == nil {
			loc = e.Loc()
		}
		return err(loc, "both rhs and lhs non-scalar/non-ground")
	}
	return nil
}

func checkOperand(op, t *ast.Term) *ast.Error {
	if t == nil {
		return err(op.Loc(), "%v: missing operand", op)
	}
	if call, ok := t.Value.(ast.Call); ok {
		loc := op.Loc()
		if loc == nil {
			loc = call[0].Loc()
		}
		return err(loc, "%v: nested call operand: %v", op, t)
	}
	return nil
}

func err(loc *ast.Location, f string, vs ...any) *ast.Error {
	return ast.NewError(code, loc, f, vs...)
}

type Details struct {
	Extra string `json:"details"`
}

func (d *Details) Lines() []string {
	return []string{d.Extra}
}

func withDetails(err *ast.Error, dets string) *ast.Error {
	err.Details = &Details{Extra: dets}
	return err
}

func findDetails(partialRef ast.Ref, sup []*ast.Module) string {
	for i := range sup {
		count := 0
		for j := range sup[i].Rules {
			if r := sup[i].Rules[j]; r.Ref().Equal(partialRef) {
				count++
				switch {
				case r.Default:
					return fmt.Sprintf("use of default rule in %v", ast.DefaultRootRef.Concat(r.Ref()[2:]))
				case count > 1:
					return fmt.Sprintf("use of multi-value rule in %v", ast.DefaultRootRef.Concat(r.Ref()[2:]))
				}
			}
		}
	}
	return ""
}
