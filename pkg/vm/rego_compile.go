//go:build use_opa_fork

package vm

import (
	"fmt"
	go_strings "strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	opa_inmem "github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"

	"github.com/styrainc/enterprise-opa-private/pkg/compile"
	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func regoCompileBuiltin(outer, state *State, args []Value) error {
	if isUndefinedType(args[0]) {
		return nil
	}
	obj := args[0]

	if isObj, err := state.ValueOps().IsObject(state.Globals.Ctx, obj); err != nil {
		return err
	} else if !isObj {
		x, err := state.ValueOps().ToAST(state.Globals.Ctx, obj)
		if err != nil {
			return err
		}
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(1, x, "object").Error(),
		})
		return nil
	}
	opts, err := state.ValueOps().ToInterface(state.Globals.Ctx, obj)
	if err != nil {
		return err
	}

	o := opts.(map[string]any)
	query, _ := o["query"].(string)
	target, _ := o["target"].(string)
	mappings, _ := o["mappings"].(map[string]any)

	raise, ok := o["raise_error"].(bool)
	if !ok { // unset, default to true like sql.send
		raise = true
	}

	comp, ok := ast.CompilerFromContext(state.Globals.Ctx)
	if !ok {
		return nil
	}
	queryRef := ast.MustParseRef(query)
	parsedUnknowns, errs := compile.ExtractUnknownsFromAnnotations(comp, queryRef)
	if errs != nil {
		state.SetReturnValue(Unused, astErrorsToObject(state, errs))
		return nil
	}

	input, data := outer.Local(Input), outer.Local(Data)
	dv, err := state.ValueOps().ToInterface(state.Globals.Ctx, data)
	if err != nil {
		return err
	}
	// NOTE(sr): Fiddling with BJSON was too cumbersome when the ordinary store
	// did the trick just as well. We can revisit.
	store := opa_inmem.NewFromObject(dv.(map[string]any))

	r := []func(*rego.Rego){
		rego.EvalMode(ast.EvalModeTopdown), // override to ensure that rule indices are built
		rego.Compiler(comp),
		rego.Store(store),
		rego.ParsedQuery(ast.NewBody(ast.NewExpr(ast.NewTerm(queryRef)))),
		rego.NondeterministicBuiltins(true),
		rego.UnsafeBuiltins(map[string]struct{}{ast.HTTPSend.Name: {}}),
	}

	// this ensures that the modules are compiled again, building the rule indices
	for _, mod := range comp.Modules {
		r = append(r, rego.ParsedModule(mod))
	}

	prep, err := rego.New(r...).PrepareForPartial(state.Globals.Ctx)
	if err != nil {
		return err
	}

	evalOpts := []rego.EvalOption{
		rego.EvalParsedUnknowns(parsedUnknowns),
		rego.EvalNondeterministicBuiltins(true),
	}
	if input != nil {
		iv, err := castJSON(state.Globals.Ctx, input)
		if err != nil {
			return err
		}
		evalOpts = append(evalOpts, rego.EvalParsedInput(iv.AST()))
	}

	pq, err := prep.Partial(state.Globals.Ctx, evalOpts...)
	if err != nil {
		return nil
	}
	if target == "" {
		return nil // no need to support legacy PE
	}
	tgt, dialect, _ := go_strings.Cut(target, "+")
	c, err := compile.NewConstraints(tgt, dialect)
	if err != nil {
		return maybeRaise(state, err, "", raise)
	}

	shorts := compile.ShortsFromMappings(mappings)

	// TODO(sr): Handle multiple (simultaneous) constraints
	if errs := compile.Check(pq, compile.NewConstraintSet(c), shorts).ASTErrors(); errs != nil {
		return maybeRaiseASTErrors(state, errs, raise)
	}

	if tgt != "sql" {
		return maybeRaise(state, nil, "UCAST not suported for rego.compile() yet", raise)
	}
	if len(pq.Queries) == 0 { // unconditional NO
		return nil
	}
	sql := ""
	m, _ := mappings[dialect].(map[string]any)
	ucast := compile.BodiesToUCAST(pq.Queries, &compile.Opts{Translations: m})
	if ucast != nil { // ucast == nil means unconditional YES, for which we'll keep `sql = ""`
		sql0, err := ucast.AsSQL(dialect)
		if err != nil {
			return maybeRaise(state, err, "", raise)
		}
		sql = sql0
	}

	state.SetReturnValue(Unused, state.ValueOps().MakeObject().Insert(
		state.ValueOps().MakeString("sql"),
		state.ValueOps().MakeString(sql),
	))
	return nil
}

func astErrorsToObject(state *State, errs []*ast.Error) fjson.Json {
	var e any = state.ValueOps().MakeSet()
	for i := range errs {
		ei, _ := state.ValueOps().FromInterface(state.Globals.Ctx, errs[i])
		e, _ = state.ValueOps().SetAdd(state.Globals.Ctx, e.(fjson.Set), ei)
	}
	return state.ValueOps().MakeObject().Insert(
		state.ValueOps().MakeString("errors"),
		e.(fjson.Set),
	)
}

func maybeRaise(state *State, err error, errString string, raise bool) error {
	if raise {
		return err
	}
	s := errString
	if err != nil {
		s = err.Error()
	}
	state.SetReturnValue(Unused, state.ValueOps().MakeObject().Insert(
		state.ValueOps().MakeString("error"),
		state.ValueOps().MakeString(s),
	))
	return nil
}

func maybeRaiseASTErrors(state *State, errs []*ast.Error, raise bool) error {
	if !raise {
		state.SetReturnValue(Unused, astErrorsToObject(state, errs))
		return nil
	}
	return aerrs{errs: errs}
}

type aerrs struct {
	errs []*ast.Error
}

func (es aerrs) Error() string {
	s := go_strings.Builder{}
	if x := len(es.errs); x > 1 {
		fmt.Fprintf(&s, "%d errors occurred during compilation:\n", x)
	} else {
		s.WriteString("1 error occurred during compilation:\n")
	}
	for i := range es.errs {
		s.WriteString(es.errs[i].Error())
	}
	return s.String()
}
