package compile

import (
	"bytes"
	"cmp"
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
	invalidUnknownCode  = "invalid_unknown"
	invalidMaskRuleCode = "invalid_mask_rule"

	prometheusHandle = "v1/compile"

	// Timer names
	timerPrepPartial                = "prep_partial"
	timerEvalConstraints            = "eval_constraints"
	timerTranslateQueries           = "translate_queries"
	timerExtractAnnotationsUnknowns = "extract_annotations_unknowns"
	timerExtractAnnotationsMask     = "extract_annotations_mask"
	timerEvalMaskRule               = "eval_mask_rule"

	unknownsCacheSize    = 500
	maskingRuleCacheSize = 500

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
	Query any            `json:"query"`
	Masks map[string]any `json:"masks,omitempty"`
}

// Incoming JSON request body structure.
type CompileRequestV1 struct {
	Input    *any      `json:"input"`
	Query    string    `json:"query"`
	Unknowns *[]string `json:"unknowns"`
	Options  struct {
		DisableInlining          []string       `json:"disableInlining,omitempty"`
		NondeterministicBuiltins bool           `json:"nondeterministicBuiltins"`
		Mappings                 map[string]any `json:"targetSQLTableMappings,omitempty"`
		TargetDialects           []string       `json:"targetDialects,omitempty"`
		MaskRule                 string         `json:"maskRule,omitempty"`
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
	cu, _ := lru.New[string, []*ast.Term](unknownsCacheSize)
	mu, _ := lru.New[string, ast.Ref](maskingRuleCacheSize)
	return &hndl{
		Logger:            l,
		unknownsCache:     cu,
		maskingRulesCache: mu,
		counterUnknownsCache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "eopa_compile_handler_unknowns_cache_lookups_total",
			Help: "The number of lookups in the unknowns cache (label \"status\" indicates hit or miss)",
		}, []string{"status"}),
		counterMaskingRulesCache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "eopa_compile_handler_masking_rules_cache_lookups_total",
			Help: "The number of lookups in the masking rules cache (label \"status\" indicates hit or miss)",
		}, []string{"status"}),
	}
}

type hndl struct {
	logging.Logger
	manager                  *plugins.Manager
	unknownsCache            *lru.Cache[string, []*ast.Term]
	maskingRulesCache        *lru.Cache[string, ast.Ref]
	counterUnknownsCache     *prometheus.CounterVec
	counterMaskingRulesCache *prometheus.CounterVec
	dl                       *logs.Plugin
	compiler                 *ast.Compiler
}

func (h *hndl) SetManager(m *plugins.Manager) error {
	extraRoute(m, "/v1/compile/{path:.+}", prometheusHandle, h.ServeHTTP)
	ctx := context.TODO()
	txn, err := m.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	if _, err := m.Store.Register(ctx, txn, storage.TriggerConfig{
		OnCommit: func(ctx context.Context, txn storage.Transaction, evt storage.TriggerEvent) {
			if evt.PolicyChanged() {
				h.Debug("purging unknowns cache for policy change")
				h.unknownsCache.Purge()
				h.maskingRulesCache.Purge()
				if err := h.prepAnnotationSet(ctx, txn); err != nil {
					h.Error("preparing annotation set: %s", err.Error())
				}
			}
		},
	}); err != nil {
		return fmt.Errorf("failed to register trigger: %w", err)
	}
	if pr := m.PrometheusRegister(); pr != nil {
		if err := pr.Register(h.counterUnknownsCache); err != nil {
			return err
		}
		if err := pr.Register(h.counterMaskingRulesCache); err != nil {
			return err
		}
	}

	h.dl = logs.Lookup(m)
	h.manager = m
	if err := h.prepAnnotationSet(ctx, txn); err != nil {
		return err
	}
	if err := m.Store.Commit(ctx, txn); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := mux.Vars(r)["path"]
	if urlPath == "" {
		writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "missing required 'path' parameter"))
		return
	}

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
	orig, request, reqErr := readInputCompilePostV1(h.manager.GetCompiler(), body, urlPath, h.manager.ParserOptions())
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
		unknowns, errs = h.unknowns(m, urlPath, request.Query)
		if errs != nil {
			writer.Error(w, http.StatusBadRequest,
				types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
					WithASTErrors(errs))
			return
		}
	}

	maskingRule := request.Options.MaskRule // always wins over annotations if provided
	if maskingRule == nil {
		var errs []*ast.Error
		maskingRule, errs = h.maskRule(m, urlPath, request.Query)
		if errs != nil {
			writer.Error(w, http.StatusBadRequest,
				types.NewErrorV1(types.CodeEvaluation, types.MsgEvaluationError).
					WithASTErrors(errs))
			return
		}
	}
	var maskResult map[string]any
	var maskResultValue ast.Value

	if maskingRule != nil {
		iqc, iqvc := getCaches(h.manager)
		m.Timer(timerEvalMaskRule).Start()

		opts := []func(*rego.Rego){
			rego.Compiler(comp),
			rego.Store(h.manager.Store),
			rego.Transaction(txn), // piggyback on previously opened read transaction.
			rego.Query(maskingRule.String()),
			rego.Instrument(includeInstrumentation),
			rego.Metrics(m),
			rego.Runtime(h.manager.Info),
			rego.UnsafeBuiltins(unsafeBuiltinsMap),
			rego.InterQueryBuiltinCache(iqc),
			rego.InterQueryBuiltinValueCache(iqvc),
			rego.PrintHook(h.manager.PrintHook()),
			// rego.QueryTracer(tracer),
			// rego.DistributedTracingOpts(s.distributedTracingOpts), // Not available in EOPA's handler.
		}

		maskResultValue, err = h.EvalMaskingRule(ctx, txn, request.Input, opts)
		if err != nil {
			switch err := err.(type) {
			case ast.Errors:
				writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, types.MsgCompileModuleError).WithASTErrors(err))
			default:
				writer.ErrorAuto(w, err)
			}
			return
		}
		if maskResultValue != nil {
			if err := util.Unmarshal([]byte(maskResultValue.String()), &maskResult); err != nil {
				h.Error("failed to round-trip mask body: %v", err)
				return
			}
		}

		m.Timer(timerEvalMaskRule).Stop()
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
	orig.Options.NondeterministicBuiltins = cmp.Or(
		target == "ucast",
		target == "sql",
		target == "multi",
		orig.Options.NondeterministicBuiltins,
	)

	iqc, iqvc := getCaches(h.manager)

	opts := []func(*rego.Rego){
		rego.Compiler(comp),
		rego.Store(h.manager.Store),
		rego.Transaction(txn),
		rego.ParsedQuery(request.Query),
		rego.DisableInlining(orig.Options.DisableInlining),
		rego.QueryTracer(buf),
		rego.Instrument(includeInstrumentation),
		rego.Metrics(m),
		rego.NondeterministicBuiltins(orig.Options.NondeterministicBuiltins),
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
		rego.EvalNondeterministicBuiltins(orig.Options.NondeterministicBuiltins),
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
	multi := make([][2]string, len(orig.Options.TargetDialects))
	var constr *ConstraintSet
	switch target {
	case "multi":
		constrs := make([]*Constraint, len(orig.Options.TargetDialects))
		for i, targetTuple := range orig.Options.TargetDialects {
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
	shorts := ShortsFromMappings(orig.Options.Mappings)

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
			mappings, err := lookupMappings(orig.Options.Mappings, target, dialect)
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
				targets.UCAST = &CompileResult{Query: r, Masks: maskResult}
			case "sql":
				sql, err := queriesToSQL(pq.Queries, mappings, dialect)
				if err != nil {
					writer.ErrorAuto(w, err) // TODO(sr): this isn't the best error we can create -- it'll be a 500 in the end, I think.
					return
				}
				switch dialect {
				case "postgresql":
					targets.Postgres = &CompileResult{Query: sql, Masks: maskResult}
				case "mysql":
					targets.MySQL = &CompileResult{Query: sql, Masks: maskResult}
				case "sqlserver":
					targets.MSSQL = &CompileResult{Query: sql, Masks: maskResult}
				case "sqlite":
					targets.SQLite = &CompileResult{Query: sql, Masks: maskResult}
				}
			}
		}

		t0 := any(targets)
		result.Result = &t0

	default:
		mappings, err := lookupMappings(orig.Options.Mappings, target, dialect)
		if err != nil {
			writer.Error(w, http.StatusBadRequest, types.NewErrorV1(types.CodeInvalidParameter, "invalid mappings"))
			return
		}

		if pq.Queries != nil { // not unconditional NO
			switch target {
			case "ucast":
				r := queriesToUCAST(pq.Queries, mappings)
				r0 := any(CompileResult{Query: r, Masks: maskResult})
				result.Result = &r0
			case "sql":
				sql, err := queriesToSQL(pq.Queries, mappings, dialect)
				if err != nil {
					writer.ErrorAuto(w, err) // TODO(sr): this isn't the best error we can create -- it'll be a 500 in the end, I think.
					return
				}
				s0 := any(CompileResult{Query: sql, Masks: maskResult})
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

		info, err := dlog(ctx, urlPath, result.Result, orig, request, unk, maskingRule.String(), m, h.manager.Store, txn)
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

func (h *hndl) unknowns(m metrics.Metrics, path string, query ast.Body) ([]*ast.Term, []*ast.Error) {
	key := path
	unknowns, ok := h.unknownsCache.Get(key)
	if ok {
		h.counterUnknownsCache.WithLabelValues("hit").Inc()
		return unknowns, nil
	}
	h.counterUnknownsCache.WithLabelValues("miss").Inc()
	m.Timer(timerExtractAnnotationsUnknowns).Start()
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
	parsedUnknowns, errs := ExtractUnknownsFromAnnotations(h.compiler, queryRef)
	if errs != nil {
		return nil, errs
	}
	unknowns = parsedUnknowns
	m.Timer(timerExtractAnnotationsUnknowns).Stop()

	h.unknownsCache.Add(key, unknowns)
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

// Processed, internal version of the compile request.
type compileRequest struct {
	Query    ast.Body
	Input    ast.Value
	Unknowns []*ast.Term
	Options  compileRequestOptions
}

type compileRequestOptions struct {
	MaskRule  ast.Ref   `json:"maskRule,omitempty"`
	MaskInput ast.Value `json:"maskInput,omitempty"`
}

func readInputCompilePostV1(comp *ast.Compiler, reqBytes []byte, urlPath string, queryParserOptions ast.ParserOptions) (*CompileRequestV1, *compileRequest, *types.ErrorV1) {
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

	var maskRuleRef ast.Ref
	if request.Options.MaskRule != "" {
		maskPath := request.Options.MaskRule
		if !strings.HasPrefix(request.Options.MaskRule, "data.") {
			// If the mask_rule is not a data ref try adding package prefix from URL path.
			dataFiltersRuleRef := stringPathToDataRef(urlPath)
			maskPath = dataFiltersRuleRef[:len(dataFiltersRuleRef)-1].String() + "." + request.Options.MaskRule
		}
		maskRuleRef, err = ast.ParseRef(maskPath)
		if err != nil {
			hint := fuzzyRuleNameMatchHint(comp, request.Options.MaskRule)
			return nil, nil, types.NewErrorV1(types.CodeInvalidParameter, "error(s) occurred while parsing mask_rule name: %s", hint)
		}
	}

	return &request, &compileRequest{
		Query:    query,
		Input:    input,
		Unknowns: unknowns,
		Options: compileRequestOptions{
			MaskRule: maskRuleRef,
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

// Modeled after (*hndl).unknowns.
func (h *hndl) maskRule(m metrics.Metrics, path string, query ast.Body) (ast.Ref, []*ast.Error) {
	key := path
	mr, ok := h.maskingRulesCache.Get(key)
	if ok {
		h.counterMaskingRulesCache.WithLabelValues("hit").Inc()
		return mr, nil
	}
	h.counterMaskingRulesCache.WithLabelValues("miss").Inc()

	m.Timer(timerExtractAnnotationsMask).Start()
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
	parsedMaskingRule, err := ExtractMaskRuleRefFromAnnotations(h.compiler, queryRef)
	if err != nil {
		return parsedMaskingRule, []*ast.Error{err}
	}
	mr = parsedMaskingRule
	m.Timer(timerExtractAnnotationsMask).Stop()

	h.maskingRulesCache.Add(key, parsedMaskingRule)
	return mr, nil
}

func ExtractMaskRuleRefFromAnnotations(comp *ast.Compiler, ref ast.Ref) (ast.Ref, *ast.Error) {
	// find ast.Rule for ref
	rules := comp.GetRulesExact(ref)
	if len(rules) == 0 {
		return nil, nil
	}
	rule := rules[0] // rule scope doesn't make sense here, so it doesn't matter which rule we use
	return maskRuleFromAnnotationsSet(comp.GetAnnotationSet(), comp, rule)
}

func maskRuleFromAnnotationsSet(as *ast.AnnotationSet, comp *ast.Compiler, rule *ast.Rule) (ast.Ref, *ast.Error) {
	if as == nil {
		return nil, nil
	}

	for _, ar := range as.Chain(rule) {
		ann := ar.Annotations
		if ann == nil {
			continue
		}
		// If the mask_rule key is present, validate and parse it.
		if maskRule, ok := ann.Custom["mask_rule"]; ok {
			if s, ok := maskRule.(string); ok {
				maskPath := s
				if !strings.HasPrefix(s, "data.") {
					// If the mask_rule is not a data ref try adding package prefix.
					maskPath = rule.Module.Package.Path.String() + "." + s
				}
				maskRuleRef, err := ast.ParseRef(maskPath)
				if err != nil {
					hint := fuzzyRuleNameMatchHint(comp, s)
					return nil, ast.NewError(invalidMaskRuleCode, ann.Loc(), "mask_rule was not a valid ref: %s", hint)
				}
				return maskRuleRef, nil
			}
			return nil, ast.NewError(invalidMaskRuleCode, ann.Loc(), "mask_rule must be a valid ref string: %v", maskRule)
		}
	}

	return nil, nil // No mask rule found.
}

// Unlike in the OPA HTTP server, errors around eval are propagated upward, and handled there.
func (h *hndl) EvalMaskingRule(ctx context.Context, txn storage.Transaction, input ast.Value, opts []func(*rego.Rego)) (ast.Value, error) {
	rr := rego.New(opts...)

	pq, err := rr.PrepareForEval(ctx)
	if err != nil {
		return nil, err
	}
	// s.preparedEvalQueries.Insert(pqID, preparedQuery)

	evalOpts := []rego.EvalOption{
		rego.EvalTransaction(txn),
		rego.EvalParsedInput(input),
	}

	rs, err := pq.Eval(
		ctx,
		evalOpts...,
	)
	if err != nil {
		return nil, err
	}

	if len(rs) == 0 {
		return nil, nil
	}

	return ast.InterfaceToValue(rs[0].Expressions[0].Value)
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

// Returns a list of similar rule names that might match the input string.
// Warning(philip): This is expensive, as the cost grows linearly with the
// number of rules present on the compiler. It should be used only for
// error messages.
func fuzzyRuleNameMatchHint(comp *ast.Compiler, input string) string {
	rules := comp.GetRules(ast.Ref{ast.DefaultRootDocument})
	ruleNames := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.Default {
			continue
		}
		ruleNames = append(ruleNames, rule.Module.Package.Path.String()+"."+rule.Head.Name.String())
	}
	closest := levenshtein.ClosestStrings(65536, input, slices.Values(ruleNames))
	proposals := slices.Compact(closest)

	var msg string
	switch len(proposals) {
	case 0:
		return ""
	case 1:
		msg = fmt.Sprintf("%s undefined, did you mean %s?", input, proposals[0])
	default:
		msg = fmt.Sprintf("%s undefined, did you mean one of %v?", input, proposals)
	}
	return msg
}

func cloneCompiler(c *ast.Compiler) *ast.Compiler {
	return ast.NewCompiler().
		WithDefaultRegoVersion(c.DefaultRegoVersion()).
		WithCapabilities(c.Capabilities())
}

func prepareAnnotations(
	ctx context.Context,
	comp *ast.Compiler,
	store storage.Store,
	txn storage.Transaction,
	po ast.ParserOptions,
) (*ast.Compiler, ast.Errors) {
	var errs []*ast.Error
	mods, err := store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, append(errs, ast.NewError(invalidUnknownCode, nil, "failed to list policies for annotation set: %s", err))
	}
	po.ProcessAnnotation = true
	po.RegoVersion = comp.DefaultRegoVersion()
	modules := make(map[string]*ast.Module, len(mods))
	for _, module := range mods {
		vsn, ok := comp.Modules[module]
		if ok { // NB(sr): I think this should be impossible. Let's try not to panic, and fall back to the default if it _does happen_.
			po.RegoVersion = vsn.RegoVersion()
		}
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

	comp0 := cloneCompiler(comp)
	comp0.WithPathConflictsCheck(storage.NonEmpty(ctx, store, txn)).Compile(modules)
	if len(comp0.Errors) > 0 {
		return nil, comp0.Errors
	}
	return comp0, nil
}

func (h *hndl) prepAnnotationSet(ctx context.Context, txn storage.Transaction) error {
	comp, errs := prepareAnnotations(ctx, h.manager.GetCompiler(), h.manager.Store, txn, h.manager.ParserOptions())
	if len(errs) > 0 {
		return FromASTErrors(errs...)
	}
	h.compiler = comp
	return nil
}

// aerrs is a wrapper giving us an `error` from `ast.Errors`
type aerrs struct {
	errs []*ast.Error
}

func FromASTErrors(errs ...*ast.Error) error {
	return &aerrs{errs}
}

func (es *aerrs) Error() string {
	s := strings.Builder{}
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
