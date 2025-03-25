package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/server/writer"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/styrainc/enterprise-opa-private/pkg/internal/levenshtein"
)

const (
	invalidUnknownCode = "invalid_unknown"

	prometheusHandle = "v1/compile"

	// Timer names
	timerPrepPartial        = "prep_partial"
	timerEvalConstraints    = "eval_constraints"
	timerTranslateQueries   = "translate_queries"
	timerExtractAnnotations = "extract_annotations"

	unknownsCacheSize = 500

	// maxDistanceForHint is the levenshtein distance below which we'll emit a hint
	maxDistanceForHint = 3

	// These need to be kept up to date with `CompileApiKnownHeaders()` below
	multiTargetJSON  = "application/vnd.styra.multitarget+json"
	ucastAllJSON     = "application/vnd.styra.ucast.all+json"
	ucastMinimalJSON = "application/vnd.styra.ucast.minimal+json"
	ucastPrismaJSON  = "application/vnd.styra.ucast.prisma+json"
	ucastLINQJSON    = "application/vnd.styra.ucast.linq+json"
	sqlPostgresJSON  = "application/vnd.styra.sql.postgresql+json"
	sqlMySQLJSON     = "application/vnd.styra.sql.mysql+json"
	sqlSQLServerJSON = "application/vnd.styra.sql.sqlserver+json"
	sqliteJSON       = "application/vnd.styra.sql.sqlite+json"

	// back-compat
	applicationJSON = "application/json"
)

func CompileAPIKnownHeaders() []string {
	return []string{
		multiTargetJSON,
		ucastAllJSON,
		ucastMinimalJSON,
		ucastPrismaJSON,
		ucastLINQJSON,
		sqlPostgresJSON,
		sqlMySQLJSON,
		sqlSQLServerJSON,
		sqliteJSON,
	}
}

var allKnownHeaders = append(CompileAPIKnownHeaders(), applicationJSON)

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

type CompileResponseV1 struct {
	Result      *any            `json:"result,omitempty"`
	Explanation types.TraceV1   `json:"explanation,omitempty"`
	Metrics     types.MetricsV1 `json:"metrics,omitempty"`
	Hints       []Hint          `json:"hints,omitempty"`
}

type Hint struct {
	Message  string        `json:"message"`
	Location *ast.Location `json:"location,omitempty"`
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
	dl      *logs.Plugin
}

func (h *hndl) SetManager(m *plugins.Manager) error {
	extraRoute(m, "/v1/compile", prometheusHandle, h.ServeHTTP)
	extraRoute(m, "/v1/compile/{path:.+}", prometheusHandle, h.ServeHTTP)
	ctx := context.TODO()
	txn, err := m.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	if _, err := m.Store.Register(ctx, txn, storage.TriggerConfig{
		OnCommit: func(_ context.Context, _ storage.Transaction, evt storage.TriggerEvent) {
			if evt.PolicyChanged() {
				h.Debug("purging unknowns cache for policy change")
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

	h.dl = logs.Lookup(m)
	h.manager = m
	return nil
}

var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := mux.Vars(r)["path"]

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

	// NOTE(sr): We keep some fields twice: from the unparsed and from the transformed
	// request (`orig` and `request` respectively), because otherwise, we'd need to re-
	// transform the values back for including them in the decision logs.
	orig, request, reqErr := readInputCompilePostV1(body, urlPath, h.manager.ParserOptions())
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
		unknowns, errs = h.unknowns(ctx, txn, comp, m, orig.Query, request.Query)
		if errs != nil {
			writer.Error(w, http.StatusBadRequest,
				types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
					WithASTErrors(errs))
			return
		}
	}

	contentType, err := sanitizeHeader(r.Header.Get("Accept"))
	if err != nil {
		writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "Accept header: %s", err.Error()))
		return
	}

	target, dialect := targetDialect(contentType)

	// We require evaluating non-det builtins for the translated targets:
	// We're not able to meaningfully tanslate things like http.send, sql.send, or
	// io.jwt.decode_verify into SQL or UCAST, so we try to eval them out where possible.
	evalNonDet := target == "ucast" ||
		target == "sql" ||
		target == "multi" ||
		request.Options.NondeterministicBuiltins

	iqc, iqvc := getCaches(h.manager)

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

	var qt failTracer
	pq, err := prep.Partial(ctx,
		rego.EvalTransaction(txn),
		rego.EvalMetrics(m),
		rego.EvalParsedInput(request.Input),
		rego.EvalParsedUnknowns(unknowns),
		rego.EvalPrintHook(h.manager.PrintHook()),
		rego.EvalNondeterministicBuiltins(evalNonDet),
		rego.EvalInterQueryBuiltinCache(iqc),
		rego.EvalInterQueryBuiltinValueCache(iqvc),
		rego.EvalQueryTracer(&qt),
		rego.EvalRuleIndexing(false),
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
	h.Debug("queries %v", pq.Queries)
	h.Debug("support %v", pq.Support)

	result := CompileResponseV1{
		Hints: qt.Hints(unknowns),
	}

	if target == "" { // "legacy" PE request
		var i any = types.PartialEvaluationResultV1{
			Queries: pq.Queries,
			Support: pq.Support,
		}
		result.Result = &i
		fin(w, result, applicationJSON, m, includeMetrics(r), includeInstrumentation, pretty(r))
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
	shorts := ShortsFromMappings(request.Options.Mappings)

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
			SQLite   *CompileResult `json:"sqlite,omitempty"`
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
				case "sqlite":
					targets.SQLite = &CompileResult{Query: sql}
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
	fin(w, result, contentType, m, includeMetrics(r), includeInstrumentation, pretty(r))

	if h.dl != nil {
		unk := make([]string, len(unknowns))
		for i := range unknowns {
			unk[i] = unknowns[i].String()
		}

		info, err := dlog(ctx, urlPath, result.Result, orig, request, unk, m, h.manager.Store, txn)
		if err != nil {
			h.Error("failed to log decision: %v", err)
			return
		}
		h.dl.Log(ctx, info)
	}
}

func ShortsFromMappings(mappings map[string]any) Set[string] {
	shorts := NewSet[string]()
	for _, mapping := range mappings {
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
	return shorts
}

func (h *hndl) unknowns(ctx context.Context, txn storage.Transaction, comp *ast.Compiler, m metrics.Metrics, qs string, query ast.Body) ([]*ast.Term, []*ast.Error) {
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
	parsedUnknowns, errs := ExtractUnknownsFromAnnotations(comp, queryRef)
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

func fin(w http.ResponseWriter,
	result CompileResponseV1,
	contentType string,
	metrics metrics.Metrics,
	includeMetrics, includeInstrumentation, pretty bool,
) {
	if includeMetrics || includeInstrumentation {
		result.Metrics = metrics.All()
	}

	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}

	w.Header().Add("Content-Type", contentType)
	// If Encode() calls w.Write() for the first time, it'll set the HTTP status
	// to 200 OK.
	if err := enc.Encode(result); err != nil {
		writer.ErrorAuto(w, err)
		return
	}
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
	DisableInlining          []string       `json:"disableInlining,omitempty"`
	NondeterministicBuiltins bool           `json:"nondeterministicBuiltins"`
	Mappings                 map[string]any `json:"targetSQLTableMappings,omitempty"`
	TargetDialects           []string       `json:"targetDialects,omitempty"`
}

func readInputCompilePostV1(reqBytes []byte, urlPath string, queryParserOptions ast.ParserOptions) (*CompileRequestV1, *compileRequest, *types.ErrorV1) {
	var request CompileRequestV1

	if len(reqBytes) > 0 {
		if err := util.NewJSONDecoder(bytes.NewBuffer(reqBytes)).Decode(&request); err != nil {
			return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while decoding request: %v", err.Error())
		}
	}

	var query ast.Body
	var err error
	if urlPath != "" {
		query = []*ast.Expr{ast.NewExpr(ast.NewTerm(stringPathToDataRef(urlPath)))}
	} else { // attempt to parse query
		query, err = ast.ParseBodyWithOpts(request.Query, queryParserOptions)
		if err != nil {
			switch err := err.(type) {
			case ast.Errors:
				return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, types.MsgParseQueryError).WithASTErrors(err)
			default:
				return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "%v: %v", types.MsgParseQueryError, err)
			}
		} else if len(query) == 0 {
			return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "missing required 'query' value")
		}
	}

	var input ast.Value
	if request.Input != nil {
		input, err = ast.InterfaceToValue(*request.Input)
		if err != nil {
			return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while converting input: %v", err)
		}
	}

	var unknowns []*ast.Term
	if request.Unknowns != nil {
		unknowns = make([]*ast.Term, len(*request.Unknowns))
		for i, s := range *request.Unknowns {
			unknowns[i], err = ast.ParseTerm(s)
			if err != nil {
				return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while parsing unknowns: %v", err)
			}
		}
	}

	return &request, &compileRequest{
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
	comp.WithPathConflictsCheck(storage.NonEmpty(ctx, store, txn)).
		Compile(modules)
	if len(comp.Errors) > 0 {
		return nil, comp.Errors
	}
	return ExtractUnknownsFromAnnotations(comp, ref)
}

func ExtractUnknownsFromAnnotations(comp *ast.Compiler, ref ast.Ref) ([]*ast.Term, []*ast.Error) {
	// find ast.Rule for ref
	rules := comp.GetRulesExact(ref)
	if len(rules) == 0 {
		return nil, nil
	}
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

func sanitizeHeader(accept string) (string, error) {
	if accept == "" {
		return "", errors.New("missing required header")
	}

	if strings.Contains(accept, ",") {
		return "", errors.New("multiple headers not supported")
	}

	if !slices.Contains(allKnownHeaders, accept) {
		return "", fmt.Errorf("unsupported header: %s", accept)
	}

	return accept, nil
}

func targetDialect(accept string) (string, string) {
	switch accept {
	case applicationJSON:
		return "", ""
	case multiTargetJSON:
		return "multi", ""
	case ucastAllJSON:
		return "ucast", "all"
	case ucastMinimalJSON:
		return "ucast", "minimal"
	case ucastPrismaJSON:
		return "ucast", "prisma"
	case ucastLINQJSON:
		return "ucast", "linq"
	case sqlPostgresJSON:
		return "sql", "postgresql"
	case sqlMySQLJSON:
		return "sql", "mysql"
	case sqlSQLServerJSON:
		return "sql", "sqlserver"
	case sqliteJSON:
		return "sql", "sqlite"
	}

	panic("unreachable")
}

// taken from v1/server/server.go
func includeMetrics(r *http.Request) bool {
	return getBoolParam(r.URL, types.ParamMetricsV1, true)
}

func pretty(r *http.Request) bool {
	return getBoolParam(r.URL, types.ParamPrettyV1, true)
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

func stringPathToDataRef(s string) (r ast.Ref) {
	result := ast.Ref{ast.DefaultRootDocument}
	return append(result, stringPathToRef(s)...)
}

func stringPathToRef(s string) (r ast.Ref) {
	if len(s) == 0 {
		return r
	}
	p := strings.Split(s, "/")
	for _, x := range p {
		if x == "" {
			continue
		}
		if y, err := url.PathUnescape(x); err == nil {
			x = y
		}
		i, err := strconv.Atoi(x)
		if err != nil {
			r = append(r, ast.StringTerm(x))
		} else {
			r = append(r, ast.IntNumberTerm(i))
		}
	}
	return r
}

type failTracer struct {
	exprs []*ast.Expr
}

func FailTracer() *failTracer {
	return &failTracer{}
}

// Enabled always returns true if the failTracer is instantiated.
func (b *failTracer) Enabled() bool {
	return b != nil
}

func (b *failTracer) TraceEvent(evt topdown.Event) {
	if evt.Op == topdown.FailOp {
		expr, ok := evt.Node.(*ast.Expr)
		if ok {
			b.exprs = append(b.exprs, expr)
		}
	}
}

// Config returns the Tracers standard configuration
func (*failTracer) Config() topdown.TraceConfig {
	return topdown.TraceConfig{PlugLocalVars: true}
}

func (b *failTracer) Hints(unknowns []*ast.Term) []Hint {
	var hints []Hint //nolint:prealloc
	seenRefs := map[string]struct{}{}
	candidates := make([]string, 0, len(unknowns))
	for i := range unknowns {
		candidates = append(candidates, string(unknowns[i].Value.(ast.Ref)[1].Value.(ast.String)))
	}

	for _, expr := range b.exprs {
		var ref ast.Ref // when this is processed, only one input.X.Y ref is in the expression (SSA)
		switch {
		case expr.IsCall():
			for i := range 2 {
				op := expr.Operand(i)
				if r, ok := op.Value.(ast.Ref); ok && r.HasPrefix(ast.InputRootRef) {
					ref = r
				}
			}
		}
		// NOTE(sr): if we allow naked ast.Term for filter policies, they need to be handled in switch ^

		if len(ref) < 2 {
			continue
		}
		tblPart, ok := ref[1].Value.(ast.String)
		if !ok {
			continue
		}
		miss := string(tblPart)
		rs := ref[1:].String()
		if _, ok := seenRefs[rs]; ok {
			continue
		}

		closestStrings := levenshtein.ClosestStrings(maxDistanceForHint, miss, slices.Values(candidates))
		proposals := make([]ast.Ref, len(closestStrings))
		for i := range closestStrings {
			if len(ref) >= 1 {
				prop := make([]*ast.Term, 2, len(ref))
				prop[0] = ast.InputRootDocument
				prop[1] = ast.StringTerm(closestStrings[i])
				prop = append(prop, ref[2:]...)
				proposals[i] = prop
			}
		}
		var msg string
		switch len(proposals) {
		case 0:
			continue
		case 1:
			msg = fmt.Sprintf("%v undefined, did you mean %s?", ref, proposals[0])
		default:
			msg = fmt.Sprintf("%v undefined, did you mean any of %v?", ref, proposals)
		}
		hints = append(hints, Hint{
			Location: expr.Loc(),
			Message:  msg,
		})
		seenRefs[rs] = struct{}{}
	}
	return hints
}
