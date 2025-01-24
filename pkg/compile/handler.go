package compile

import (
	"bytes"
	"net/http"

	"github.com/huandu/go-sqlbuilder"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/runtime"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/server/writer"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/util"
)

const invalidUnknownCode = "invalid_unknown"

type CompileRequestV1 struct {
	Input    *interface{} `json:"input"`
	Query    string       `json:"query"`
	Unknowns *[]string    `json:"unknowns"`
	Options  struct {
		DisableInlining []string       `json:"disableInlining,omitempty"`
		Dialect         string         `json:"dialect,omitempty"`
		Mappings        map[string]any `json:"targetSQLTableMappings,omitempty"`
	} `json:"options,omitempty"`
}

type CompileHandler interface {
	http.Handler
	SetRuntime(*runtime.Runtime)
}

func Handler(l logging.Logger) CompileHandler {
	return &hndl{Logger: l}
}

type hndl struct {
	logging.Logger
	rt *runtime.Runtime
}

func (h *hndl) SetRuntime(rt *runtime.Runtime) {
	h.rt = rt
}

func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	explainMode := types.ExplainOffV1
	includeInstrumentation := false

	m := metrics.New()
	m.Timer(metrics.ServerHandler).Start()
	m.Timer(metrics.RegoQueryParse).Start()

	// decompress the input if sent as zip
	body, err := util.ReadMaybeCompressedBody(r)
	if err != nil {
		writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "could not decompress the body"))
		return
	}

	request, reqErr := readInputCompilePostV1(body, h.rt.Manager.ParserOptions())
	if reqErr != nil {
		writer.Error(w, http.StatusBadRequest, reqErr)
		return
	}

	m.Timer(metrics.RegoQueryParse).Stop()

	c := storage.NewContext().WithMetrics(m)
	txn, err := h.rt.Store.NewTransaction(ctx, storage.TransactionParams{Context: c})
	if err != nil {
		writer.ErrorAuto(w, err)
		return
	}

	defer h.rt.Store.Abort(ctx, txn)

	var buf *topdown.BufferTracer
	if explainMode != types.ExplainOffV1 {
		buf = topdown.NewBufferTracer()
	}

	comp := h.rt.Manager.GetCompiler()
	unknowns := request.Unknowns
	if len(unknowns) == 0 { // Read unknowns from metadata
		parsedUnknowns, errs := parseUnknownsFromAnnotations(comp)
		if errs != nil {
			writer.Error(w, http.StatusBadRequest,
				types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
					WithASTErrors(errs))
			return
		}
		unknowns = parsedUnknowns
	}

	eval := rego.New(
		rego.Compiler(comp),
		rego.Store(h.rt.Store),
		rego.Transaction(txn),
		rego.ParsedQuery(request.Query),
		rego.DisableInlining(request.Options.DisableInlining),
		rego.QueryTracer(buf),
		rego.Instrument(includeInstrumentation),
		rego.Metrics(m),
		// rego.Runtime(s.runtime),
		// rego.UnsafeBuiltins(unsafeBuiltinsMap),
		// rego.InterQueryBuiltinCache(s.interQueryBuiltinCache),
		// rego.InterQueryBuiltinValueCache(s.interQueryBuiltinValueCache),
		rego.PrintHook(h.rt.Manager.PrintHook()),
	)

	prep, err := eval.PrepareForPartial(ctx)
	if err != nil {
		switch err := err.(type) {
		case ast.Errors:
			writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, types.MsgCompileModuleError).WithASTErrors(err))
		default:
			writer.ErrorAuto(w, err)
		}
		return
	}

	pq, err := prep.Partial(ctx,
		rego.EvalParsedInput(request.Input),
		rego.EvalParsedUnknowns(unknowns),
		rego.EvalPrintHook(h.rt.Manager.PrintHook()),
	)
	if err != nil {
		switch err := err.(type) {
		case ast.Errors:
			writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, types.MsgCompileModuleError).WithASTErrors(err))
		default:
			writer.ErrorAuto(w, err)
		}
		return
	}

	m.Timer(metrics.ServerHandler).Stop()

	result := types.CompileResponseV1{}

	if includeInstrumentation {
		result.Metrics = m.All()
	}

	var i any = types.PartialEvaluationResultV1{
		Queries: pq.Queries,
		Support: pq.Support,
	}

	var constr *Constraint
	switch r.Header.Get("Accept") {
	case "application/vnd.styra.ucast+json":
		constr = NewConstraints("ucast", request.Options.Dialect)
	case "application/vnd.styra.sql+json":
		constr = NewConstraints("sql", request.Options.Dialect)
	}
	h.Logger.Debug("queries %v", pq.Queries)
	h.Logger.Debug("support %v", pq.Support)
	if errs := Check(pq, constr).ASTErrors(); errs != nil {
		writer.Error(w, http.StatusBadRequest,
			types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
				WithASTErrors(errs))
		return
	}

	if pq.Queries != nil { // not unconditional NO
		switch r.Header.Get("Accept") {
		case "application/vnd.styra.ucast+json":
			opts := &Opts{
				Translations: request.Options.Mappings,
			}
			var a any
			ucast := BodiesToUCAST(pq.Queries, opts)
			if ucast == nil { // unconditional YES
				// NOTE(sr): we cannot encode "no conditions" in ucast.UCASTNode{}, so we return an empty map
				a = struct{}{}
			} else {
				a = any(ucast)
			}
			result.Result = &a
		case "application/vnd.styra.sql+json":
			opts := &Opts{
				Translations: request.Options.Mappings,
			}
			ucast := BodiesToUCAST(pq.Queries, opts)
			if ucast == nil { // unconditional YES
				s := any("")
				result.Result = &s
				break
			}
			// TODO(sr): Hide away the sqlbuilder calls
			var fl sqlbuilder.Flavor
			switch request.Options.Dialect {
			case "mysql":
				fl = sqlbuilder.MySQL
			case "sqlite":
				fl = sqlbuilder.SQLite
			case "postgres":
				fl = sqlbuilder.PostgreSQL
			case "sqlserver":
				fl = sqlbuilder.SQLServer
			default:
				writer.Error(w, http.StatusBadRequest,
					types.NewErrorV1(types.CodeInvalidParameter, "unsupported dialect: %s", request.Options.Dialect))
				return
			}

			cond := sqlbuilder.NewCond()
			where := sqlbuilder.NewWhereClause()
			clauses, err := ucast.AsSQL(cond, request.Options.Dialect)
			if err != nil {
				writer.ErrorAuto(w, err)
				return
			}
			where.AddWhereExpr(cond.Args, clauses)
			s, args := where.BuildWithFlavor(fl)
			sql, err := fl.Interpolate(s, args)
			if err != nil {
				writer.ErrorAuto(w, err)
				return
			}
			r := any(sql)
			result.Result = &r

		default:
			result.Result = &i
		}
	}

	writer.JSONOK(w, result, true)
}

type compileRequest struct {
	Query    ast.Body
	Input    ast.Value
	Unknowns []*ast.Term
	Options  compileRequestOptions
}

type compileRequestOptions struct {
	DisableInlining []string
	Dialect         string
	Mappings        map[string]any
}

func readInputCompilePostV1(reqBytes []byte, queryParserOptions ast.ParserOptions) (*compileRequest, *types.ErrorV1) {
	var request CompileRequestV1

	err := util.NewJSONDecoder(bytes.NewBuffer(reqBytes)).Decode(&request)
	if err != nil {
		return nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while decoding request: %v", err.Error())
	}

	query, err := ast.ParseBodyWithOpts(request.Query, queryParserOptions)
	if err != nil {
		switch err := err.(type) {
		case ast.Errors:
			return nil, types.NewErrorV1(types.CodeInvalidParameter, types.MsgParseQueryError).WithASTErrors(err)
		default:
			return nil, types.NewErrorV1(types.CodeInvalidParameter, "%v: %v", types.MsgParseQueryError, err)
		}
	} else if len(query) == 0 {
		return nil, types.NewErrorV1(types.CodeInvalidParameter, "missing required 'query' value")
	}

	var input ast.Value
	if request.Input != nil {
		input, err = ast.InterfaceToValue(*request.Input)
		if err != nil {
			return nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while converting input: %v", err)
		}
	}

	var unknowns []*ast.Term
	if request.Unknowns != nil {
		unknowns = make([]*ast.Term, len(*request.Unknowns))
		for i, s := range *request.Unknowns {
			unknowns[i], err = ast.ParseTerm(s)
			if err != nil {
				return nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while parsing unknowns: %v", err)
			}
		}
	}

	result := &compileRequest{
		Query:    query,
		Input:    input,
		Unknowns: unknowns,
		Options: compileRequestOptions{
			DisableInlining: request.Options.DisableInlining,
			Mappings:        request.Options.Mappings,
			Dialect:         request.Options.Dialect,
		},
	}

	return result, nil
}

func parseUnknownsFromAnnotations(comp *ast.Compiler) ([]*ast.Term, []*ast.Error) {
	var unknowns []*ast.Term
	var errs []*ast.Error

	if as := comp.GetAnnotationSet(); as != nil {
		for _, ar := range as.Flatten() {
			ann := ar.Annotations
			unk, ok := ann.Custom["unknowns"]
			if !ok {
				continue
			}
			unkArray, ok := unk.([]any)
			if !ok {
				continue
			}
			for _, u := range unkArray {
				s, ok := u.(string)
				if !ok {
					continue
				}
				ref, err := ast.ParseRef(s)
				if err != nil {
					errs = append(errs, ast.NewError(invalidUnknownCode, ann.Loc(), "unknowns must be valid refs: %s", s))
				} else if ref.HasPrefix(ast.DefaultRootRef) || ref.HasPrefix(ast.InputRootRef) {
					unknowns = append(unknowns, ast.NewTerm(ref))
				} else {
					errs = append(errs, ast.NewError(invalidUnknownCode, ann.Loc(), "unknowns must be prefixed with `input` or `data`: %v", ref))
				}
			}
		}
	}

	return unknowns, errs
}
