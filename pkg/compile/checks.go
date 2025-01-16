package compile

import (
	"cmp"
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

type checker struct {
	constraints *Constraint
	res         *Results
}

func (c *checker) Results() *Results {
	return c.res
}

func Check(pq *rego.PartialQueries, constraints *Constraint) *Results {
	check := checker{
		constraints: constraints,
		res:         &Results{},
	}
	for i := range pq.Queries {
		check.Query(pq.Queries[i], pq.Support)
	}
	// NOTE(sr): So far, we've gotten better error locations from the refs into
	// support modules. The support modules themselves are surprisingly useless
	// for that.
	// for i := range pq.Support {
	// 	checkSupport(pq.Support[i], &res)
	// }
	return check.Results()
}

func (c *checker) Query(q ast.Body, sup []*ast.Module) {
	for j := range q {
		for i := range queryChecks {
			if err := queryChecks[i](c, q[j], sup); err != nil {
				c.res.errs = append(c.res.errs, err)
			}
		}
	}
}

var queryChecks = [...]func(*checker, *ast.Expr, []*ast.Module) *ast.Error{
	checkCall,
	checkBuiltins,
}

var partialPrefix = ast.MustParseRef("data.partial")

func checkCall(c *checker, e *ast.Expr, sup []*ast.Module) *ast.Error {
	switch {
	case e.Negated:
		if c.constraints.Supports("not") && e.IsCall() { // IsCall gives us comparisons etc, and rules out naked data refs
			break // OK
		}
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

// some builtins need their names replaced for nicer errors
var replacements = map[string]string{
	"internal.member_2": "in",
}

func checkBuiltins(c *checker, e *ast.Expr, _ []*ast.Module) *ast.Error {
	if len(e.With) > 0 { // Ignore expression, we'll already have recorded errors through checkCalls.
		return nil
	}
	op := e.OperatorTerm()
	if op == nil {
		return nil
	}
	loc := op.Loc()
	ref := op.Value.(ast.Ref)
	op0 := ref.String()

	unknownMustBeFirst := false

	switch {
	case op0 == ast.Equality.Name:
	case op0 == ast.NotEqual.Name:
	case op0 == ast.LessThan.Name:
	case op0 == ast.LessThanEq.Name:
	case op0 == ast.GreaterThan.Name:
	case op0 == ast.GreaterThanEq.Name:
	case op0 == ast.StartsWith.Name ||
		op0 == ast.EndsWith.Name ||
		op0 == ast.Contains.Name ||
		op0 == ast.Member.Name:
		unknownMustBeFirst = true

		// Below there are only error cases
	case op0 == ast.MemberWithKey.Name:
		return err(loc, "invalid use of \"... in ...\"")
	case ref.HasPrefix(ast.DefaultRootRef):
		// TODO(sr): point to function with else -- but we don't have the full rego yet
		return withDetails(err(e.Loc(), "invalid data reference \"%v\"", e),
			fmt.Sprintf("has function \"%v(...)\" an `else`?", ref),
		)
	default:
		return err(loc, "invalid builtin `%v`", op)
	}

	// Also check that our target+variant allows this builtin
	if !c.constraints.Builtins.Contains(op0) {
		return err(loc, "invalid builtin `%v` for %v",
			cmp.Or(replacements[op0], op0),
			c.constraints)
	}

	// all our allowed builtins have two operands
	for i := range 2 {
		if err := checkOperand(op, e.Operand(i)); err != nil {
			return err
		}
	}

	if unknownMustBeFirst {
		if _, ok := e.Operand(0).Value.(ast.Ref); !ok {
			if loc == nil {
				loc = e.Loc()
			}
			return err(loc, "rhs of %v must be known", op)
		}
	} else { // lhs or rhs needs to be ground scalar
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
