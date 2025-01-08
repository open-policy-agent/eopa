package compile

import (
	"bytes"
	"net/http"

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

	request, reqErr := readInputCompilePostV1(body)
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

	eval := rego.New(
		rego.Compiler(h.rt.Manager.GetCompiler()),
		rego.Store(h.rt.Store),
		rego.Transaction(txn),
		rego.ParsedQuery(request.Query),
		rego.ParsedInput(request.Input),
		rego.ParsedUnknowns(request.Unknowns),
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

	pq, err := eval.Partial(ctx)
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

	h.Logger.Debug("queries %v", pq.Queries)
	h.Logger.Debug("support %v", pq.Support)
	if errs := Check(pq).ASTErrors(); errs != nil {
		writer.Error(w, http.StatusBadRequest,
			types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
				WithASTErrors(errs))
		return
	}

	result.Result = &i

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
}

func readInputCompilePostV1(reqBytes []byte) (*compileRequest, *types.ErrorV1) {
	var request types.CompileRequestV1

	err := util.NewJSONDecoder(bytes.NewBuffer(reqBytes)).Decode(&request)
	if err != nil {
		return nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while decoding request: %v", err.Error())
	}

	query, err := ast.ParseBody(request.Query)
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
		},
	}

	return result, nil
}
