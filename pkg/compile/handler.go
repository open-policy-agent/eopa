package compile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/server/writer"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/util"
)

const (
	invalidUnknownCode = "invalid_unknown"

	multiTargetJSON  = "application/vnd.styra.multitarget+json"
	ucastAllJSON     = "application/vnd.styra.ucast.all+json"
	ucastMinimalJSON = "application/vnd.styra.ucast.minimal+json"
	ucastPrismaJSON  = "application/vnd.styra.ucast.prisma+json"
	ucastLINQJSON    = "application/vnd.styra.ucast.linq+json"
	sqlPostgresJSON  = "application/vnd.styra.sql.postgresql+json"
	sqlMySQLJSON     = "application/vnd.styra.sql.mysql+json"
	sqlSQLServerJSON = "application/vnd.styra.sql.sqlserver+json"

	prometheusHandle = "v1/compile"

	// Timer names
	timerPrepPartial        = "prep_partial"
	timerEvalConstraints    = "eval_constraints"
	timerTranslateQueries   = "translate_queries"
	timerExtractAnnotations = "extract_annotations"

	unknownsCacheSize = 500
)

type CompileResult struct {
	Query any `json:"query"`
}

type CompileRequestV1 struct {
	Input    *any      `json:"input"`
	Query    string    `json:"query"`
	Unknowns *[]string `json:"unknowns"`
	Options  struct {
		DisableInlining          []string       `json:"disableInlining,omitempty"`
		NondeterministicBuiltins bool           `json:"nondeterministicBuiltins"`
		Mappings                 map[string]any `json:"targetSQLTableMappings,omitempty"`
		TargetDialects           []string       `json:"targetDialects,omitempty"`
	} `json:"options,omitempty"`
}

type CompileHandler interface {
	http.Handler
	SetManager(*plugins.Manager) error
}

func Handler(l logging.Logger) CompileHandler {
	c, _ := lru.New[string, []*ast.Term](unknownsCacheSize)
	return &hndl{Logger: l,
		cache: c,
		counter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "eopa_compile_handler_unknowns_cache_lookups_total",
			Help: "The number of lookups in the unknowns cache (label \"status\" indicates hit or miss)",
		}, []string{"status"}),
	}
}

type hndl struct {
	logging.Logger
	manager *plugins.Manager
	cache   *lru.Cache[string, []*ast.Term]
	counter *prometheus.CounterVec
}

func (h *hndl) SetManager(m *plugins.Manager) error {
	m.ExtraRoute("/v1/compile", prometheusHandle, h.ServeHTTP)
	ctx := context.TODO()
	txn, err := m.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	if _, err := m.Store.Register(ctx, txn, storage.TriggerConfig{
		OnCommit: func(_ context.Context, _ storage.Transaction, evt storage.TriggerEvent) {
			if evt.PolicyChanged() {
				h.Logger.Debug("purging unknowns cache for policy change")
				h.cache.Purge()
			}
		}}); err != nil {
		return fmt.Errorf("failed to register trigger: %w", err)
	}
	if err := m.Store.Commit(ctx, txn); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	if pr := m.PrometheusRegister(); pr != nil {
		if err := pr.Register(h.counter); err != nil {
			return err
		}
	}

	h.manager = m
	return nil
}

var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	explainMode := getExplain(r.URL.Query()[types.ParamExplainV1], types.ExplainOffV1)
	includeInstrumentation := getBoolParam(r.URL, types.ParamInstrumentV1, true)

	m := metrics.New()
	m.Timer(metrics.ServerHandler).Start()
	m.Timer(metrics.RegoQueryParse).Start()

	// decompress the input if sent as zip
	body, err := util.ReadMaybeCompressedBody(r)
	if err != nil {
		writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "could not decompress the body: %v", err))
		return
	}

	request, reqErr := readInputCompilePostV1(body, h.manager.ParserOptions())
	if reqErr != nil {
		writer.Error(w, http.StatusBadRequest, reqErr)
		return
	}

	m.Timer(metrics.RegoQueryParse).Stop()

	c := storage.NewContext().WithMetrics(m)
	txn, err := h.manager.Store.NewTransaction(ctx, storage.TransactionParams{Context: c})
	if err != nil {
		writer.ErrorAuto(w, err)
		return
	}

	defer h.manager.Store.Abort(ctx, txn)
	var buf *topdown.BufferTracer
	if explainMode != types.ExplainOffV1 {
		buf = topdown.NewBufferTracer()
	}

	comp := h.manager.GetCompiler()
	unknowns := request.Unknowns // always wins over annotations if provided
	if len(unknowns) == 0 {      // check cache for unknowns
		var errs []*ast.Error
		unknowns, errs = h.unknowns(ctx, txn, comp, m, request.Query)
		if errs != nil {
			writer.Error(w, http.StatusBadRequest,
				types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
					WithASTErrors(errs))
			return
		}
	}

	target, dialect, err := parseHeader(r.Header.Get("Accept"))
	if err != nil {
		writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "Accept header: %s", err.Error()))
		return
	}

	// We require evaluating non-det builtins for the translated targets:
	// We're not able to meaningfully tanslate things like http.send, sql.send, or
	// io.jwt.decode_verify into SQL or UCAST, so we try to eval them out where possible.
	evalNonDet := target == "ucast" ||
		target == "sql" ||
		target == "multi" ||
		request.Options.NondeterministicBuiltins

	iqc, iqvc := h.manager.GetCaches()

	opts := []func(*rego.Rego){
		rego.Compiler(comp),
		rego.Store(h.manager.Store),
		rego.Transaction(txn),
		rego.ParsedQuery(request.Query),
		rego.DisableInlining(request.Options.DisableInlining),
		rego.QueryTracer(buf),
		rego.Instrument(includeInstrumentation),
		rego.Metrics(m),
		rego.NondeterministicBuiltins(evalNonDet),
		rego.Runtime(h.manager.Info),
		rego.UnsafeBuiltins(unsafeBuiltinsMap),
		rego.InterQueryBuiltinCache(iqc),
		rego.InterQueryBuiltinValueCache(iqvc),
		rego.PrintHook(h.manager.PrintHook()),
	}

	m.Timer(timerPrepPartial).Start()

	prep, err := rego.New(opts...).PrepareForPartial(ctx)
	if err != nil {
		switch err := err.(type) {
		case ast.Errors:
			writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, types.MsgCompileModuleError).WithASTErrors(err))
		default:
			writer.ErrorAuto(w, err)
		}
		return
	}

	m.Timer(timerPrepPartial).Stop()

	pq, err := prep.Partial(ctx,
		rego.EvalMetrics(m),
		rego.EvalParsedInput(request.Input),
		rego.EvalParsedUnknowns(unknowns),
		rego.EvalPrintHook(h.manager.PrintHook()),
		rego.EvalNondeterministicBuiltins(evalNonDet),
		rego.EvalInterQueryBuiltinCache(iqc),
		rego.EvalInterQueryBuiltinValueCache(iqvc),
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
	h.Logger.Debug("queries %v", pq.Queries)
	h.Logger.Debug("support %v", pq.Support)

	result := types.CompileResponseV1{}

	if target == "" { // "legacy" PE request
		var i any = types.PartialEvaluationResultV1{
			Queries: pq.Queries,
			Support: pq.Support,
		}
		result.Result = &i
		fin(w, result, m, includeMetrics(r), includeInstrumentation)
		return
	}

	// build ConstraintSet for single- and multi-target requests
	m.Timer(timerEvalConstraints).Start()
	multi := make([][2]string, len(request.Options.TargetDialects))
	var constr *ConstraintSet
	switch target {
	case "multi":
		constrs := make([]*Constraint, len(request.Options.TargetDialects))
		for i, targetTuple := range request.Options.TargetDialects {
			s := strings.Split(targetTuple, "+")
			target, dialect := s[0], s[1]
			multi[i] = [2]string{target, dialect}

			constrs[i], err = NewConstraints(target, dialect) // NewConstraints validates the tuples
			if err != nil {
				writer.Error(w, http.StatusBadRequest,
					types.NewErrorV1(types.CodeInvalidParameter, "multi-target request: %s", err.Error()))
				return
			}
		}
		constr = NewConstraintSet(constrs...)
	default:
		c, err := NewConstraints(target, dialect)
		if err != nil {
			writer.ErrorAuto(w, types.BadRequestErr(err.Error()))
			return
		}
		constr = NewConstraintSet(c)
	}

	// We collect all the mapped short names -- not per-dialect or per-target, since if
	// you use a short, you need to provide a mapping for every target.
	shorts := NewSet[string]()
	for _, mapping := range request.Options.Mappings { // dt = dialect or target
		m, ok := mapping.(map[string]any)
		if !ok {
			continue
		}
		for n, nmap := range m {
			m, ok := nmap.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := m["$table"]; ok {
				shorts = shorts.Add(n)
			}
		}
	}

	// check PE queries against constraints
	if errs := Check(pq, constr, shorts).ASTErrors(); errs != nil {
		writer.Error(w, http.StatusBadRequest,
			types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
				WithASTErrors(errs))
		return
	}
	m.Timer(timerEvalConstraints).Stop()

	m.Timer(timerTranslateQueries).Start()
	switch target {
	case "multi":
		targets := struct {
			UCAST    *CompileResult `json:"ucast,omitempty"`
			Postgres *CompileResult `json:"postgresql,omitempty"`
			MySQL    *CompileResult `json:"mysql,omitempty"`
			MSSQL    *CompileResult `json:"sqlserver,omitempty"`
		}{}

		for _, targetTuple := range multi {
			target, dialect := targetTuple[0], targetTuple[1]
			mappings, err := lookupMappings(request.Options.Mappings, target, dialect)
			if err != nil {
				writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "invalid mappings"))
				return
			}
			switch target {
			case "ucast":
				if targets.UCAST != nil {
					continue // there's only one UCAST representation, don't translate that twice
				}
				r := queriesToUCAST(pq.Queries, mappings)
				targets.UCAST = &CompileResult{Query: r}
			case "sql":
				sql, err := queriesToSQL(pq.Queries, mappings, dialect)
				if err != nil {
					writer.ErrorAuto(w, err) // TODO(sr): this isn't the best error we can create -- it'll be a 500 in the end, I think.
					return
				}
				switch dialect {
				case "postgresql":
					targets.Postgres = &CompileResult{Query: sql}
				case "mysql":
					targets.MySQL = &CompileResult{Query: sql}
				case "sqlserver":
					targets.MSSQL = &CompileResult{Query: sql}
				}
			}
		}

		t0 := any(targets)
		result.Result = &t0

	default:
		mappings, err := lookupMappings(request.Options.Mappings, target, dialect)
		if err != nil {
			writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "invalid mappings"))
			return
		}

		if pq.Queries != nil { // not unconditional NO
			switch target {
			case "ucast":
				r := queriesToUCAST(pq.Queries, mappings)
				r0 := any(CompileResult{Query: r})
				result.Result = &r0
			case "sql":
				sql, err := queriesToSQL(pq.Queries, mappings, dialect)
				if err != nil {
					writer.ErrorAuto(w, err) // TODO(sr): this isn't the best error we can create -- it'll be a 500 in the end, I think.
					return
				}
				s0 := any(CompileResult{Query: sql})
				result.Result = &s0
			}
		}
	}

	m.Timer(timerTranslateQueries).Stop()
	m.Timer(metrics.ServerHandler).Stop()
	fin(w, result, m, includeMetrics(r), includeInstrumentation)
}

func (h *hndl) unknowns(ctx context.Context, txn storage.Transaction, comp *ast.Compiler, m metrics.Metrics, query ast.Body) ([]*ast.Term, []*ast.Error) {
	qs := query.String()
	unknowns, ok := h.cache.Get(qs)
	if ok {
		h.counter.WithLabelValues("hit").Inc()
		return unknowns, nil
	}
	h.counter.WithLabelValues("miss").Inc()
	m.Timer(timerExtractAnnotations).Start()
	if len(query) != 1 {
		return nil, nil
	}
	q, ok := query[0].Terms.(*ast.Term)
	if !ok {
		return nil, nil
	}
	queryRef, ok := (*q).Value.(ast.Ref)
	if !ok {
		return nil, nil
	}
	parsedUnknowns, errs := extractUnknownsFromAnnotations(comp, queryRef)
	if errs != nil {
		return nil, errs
	}
	if parsedUnknowns == nil { // none found on the compiler, re-parse modules
		parsedUnknowns, errs = parseUnknownsFromModules(ctx, comp, h.manager.Store, txn, h.manager.ParserOptions(), queryRef)
		if errs != nil {
			return nil, errs
		}
	}
	unknowns = parsedUnknowns
	m.Timer(timerExtractAnnotations).Stop()

	h.cache.Add(qs, unknowns)
	return unknowns, nil
}

func queriesToUCAST(queries []ast.Body, mappings map[string]any) any {
	ucast := BodiesToUCAST(queries, &Opts{Translations: mappings})
	if ucast == nil { // ucast == nil means unconditional YES
		return struct{}{}
	}
	return ucast
}

func queriesToSQL(queries []ast.Body, mappings map[string]any, dialect string) (string, error) {
	sql := ""
	ucast := BodiesToUCAST(queries, &Opts{Translations: mappings})
	if ucast != nil { // ucast == nil means unconditional YES, for which we'll keep `sql = ""`
		sql0, err := ucast.AsSQL(dialect)
		if err != nil {
			return "", err
		}
		sql = sql0
	}

	return sql, nil
}

type compileRequest struct {
	Query    ast.Body
	Input    ast.Value
	Unknowns []*ast.Term
	Options  compileRequestOptions
}

type compileRequestOptions struct {
	DisableInlining          []string
	NondeterministicBuiltins bool
	Mappings                 map[string]any
	TargetDialects           []string
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

	return &compileRequest{
		Query:    query,
		Input:    input,
		Unknowns: unknowns,
		Options: compileRequestOptions{
			DisableInlining: request.Options.DisableInlining,
			Mappings:        request.Options.Mappings,
			TargetDialects:  request.Options.TargetDialects,
		},
	}, nil
}

func lookupMappings(mappings map[string]any, target, dialect string) (map[string]any, error) {
	if mappings == nil {
		return nil, nil
	}

	if md := mappings[dialect]; md != nil {
		m, ok := md.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid mappings for dialect %s", dialect)
		}
		if m != nil {
			return m, nil
		}
	}

	if mt := mappings[target]; mt != nil {
		n, ok := mt.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid mappings for target %s", target)
		}
		return n, nil
	}
	return nil, nil
}

func parseUnknownsFromModules(ctx context.Context, comp *ast.Compiler, store storage.Store, txn storage.Transaction, po ast.ParserOptions, ref ast.Ref) ([]*ast.Term, []*ast.Error) {
	var errs []*ast.Error
	mods, err := store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, append(errs, ast.NewError(invalidUnknownCode, nil, "failed to list policies for annotation set: %s", err))
	}
	po.ProcessAnnotation = true
	modules := make(map[string]*ast.Module, len(mods))
	for _, module := range mods {
		rego, err := store.GetPolicy(ctx, txn, module)
		if err != nil {
			return nil, append(errs, ast.NewError(invalidUnknownCode, nil, "failed to read module for annotation set: %s", err))
		}
		m, err := ast.ParseModuleWithOpts(module, string(rego), po)
		if err != nil {
			errs = append(errs, ast.NewError(invalidUnknownCode, nil, "failed to parse module for annotation set: %s", err))
			continue
		}
		modules[module] = m
	}
	if errs != nil {
		return nil, errs
	}
	comp.Compile(modules)
	if len(comp.Errors) > 0 {
		return nil, comp.Errors
	}
	return extractUnknownsFromAnnotations(comp, ref)
}

func extractUnknownsFromAnnotations(comp *ast.Compiler, ref ast.Ref) ([]*ast.Term, []*ast.Error) {
	// find ast.Rule for ref
	rules := comp.GetRulesExact(ref)
	rule := rules[0] // rule scope doesn't make sense here, so it doesn't matter which rule we use
	return unknownsFromAnnotationsSet(comp.GetAnnotationSet(), rule)
}

func unknownsFromAnnotationsSet(as *ast.AnnotationSet, rule *ast.Rule) ([]*ast.Term, []*ast.Error) {
	if as == nil {
		return nil, nil
	}
	var unknowns []*ast.Term
	var errs []*ast.Error

	for _, ar := range as.Chain(rule) {
		ann := ar.Annotations
		if ann == nil {
			continue
		}
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

	return unknowns, errs
}

func fin(w http.ResponseWriter, result types.CompileResponseV1,
	metrics metrics.Metrics,
	includeMetrics, includeInstrumentation bool) {
	if includeMetrics || includeInstrumentation {
		result.Metrics = metrics.All()
	}
	writer.JSONOK(w, result, true)
}

func parseHeader(accept string) (string, string, error) {
	if accept == "" {
		return "", "", errors.New("missing required header")
	}

	accepts := strings.Split(accept, ",")
	if len(accepts) != 1 {
		return "", "", errors.New("multiple headers not supported")
	}

	switch accepts[0] {
	case "application/json":
		return "", "", nil
	case multiTargetJSON:
		return "multi", "", nil
	case ucastAllJSON:
		return "ucast", "all", nil
	case ucastMinimalJSON:
		return "ucast", "minimal", nil
	case ucastPrismaJSON:
		return "ucast", "prisma", nil
	case ucastLINQJSON:
		return "ucast", "linq", nil
	case sqlPostgresJSON:
		return "sql", "postgresql", nil
	case sqlMySQLJSON:
		return "sql", "mysql", nil
	case sqlSQLServerJSON:
		return "sql", "sqlserver", nil
	}

	return "", "", fmt.Errorf("unsupported header: %s", accepts[0])
}

// taken from v1/server/server.go
func includeMetrics(r *http.Request) bool {
	return getBoolParam(r.URL, types.ParamMetricsV1, true)
}

func getBoolParam(url *url.URL, name string, ifEmpty bool) bool {

	p, ok := url.Query()[name]
	if !ok {
		return false
	}

	// Query params w/o values are represented as slice (of len 1) with an
	// empty string.
	if len(p) == 1 && p[0] == "" {
		return ifEmpty
	}

	for _, x := range p {
		if strings.ToLower(x) == "true" {
			return true
		}
	}

	return false
}

func getExplain(p []string, zero types.ExplainModeV1) types.ExplainModeV1 {
	for _, x := range p {
		switch x {
		case string(types.ExplainNotesV1):
			return types.ExplainNotesV1
		case string(types.ExplainFailsV1):
			return types.ExplainFailsV1
		case string(types.ExplainFullV1):
			return types.ExplainFullV1
		case string(types.ExplainDebugV1):
			return types.ExplainDebugV1
		}
	}
	return zero
}
