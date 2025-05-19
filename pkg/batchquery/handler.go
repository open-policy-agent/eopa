package batchquery

import (
	"context"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/plugins"
	bundlePlugin "github.com/open-policy-agent/opa/v1/plugins/bundle"
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/server"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/server/writer"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/lineage"
	"github.com/open-policy-agent/opa/v1/tracing"
	"github.com/open-policy-agent/opa/v1/version"
)

const (
	pqMaxCacheSize     = 100
	PrometheusHandle   = "v1/batch/data"
	otelDecisionIDAttr = "opa.decision_id" // OpenTelemetry attributes
)

var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

type BatchQueryHandler interface {
	http.Handler
	GetManager() *plugins.Manager
	SetManager(*plugins.Manager) error
	WithDecisionIDFactory(func() string) BatchQueryHandler
	WithDecisionLogger(logger func(context.Context, *server.Info) error) BatchQueryHandler
}

func Handler(l logging.Logger) BatchQueryHandler {
	peqc, _ := lru.New[string, *rego.PreparedEvalQuery](pqMaxCacheSize)
	return &hndl{
		Logger:                 l,
		preparedEvalQueryCache: peqc,
		counterPEQCache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "eopa_batch_query_handler_prepared_eval_query_cache_lookups_total",
			Help: "The number of lookups in the prepared eval query cache (label \"status\" indicates hit or miss)",
		}, []string{"status"}),
	}
}

type hndl struct {
	logging.Logger
	manager                *plugins.Manager
	preparedEvalQueryCache *lru.Cache[string, *rego.PreparedEvalQuery]
	counterPEQCache        *prometheus.CounterVec
	decisionLoggerFunc     func(context.Context, *server.Info) error
	decisionIDFactory      func() string
	dl                     *logs.Plugin
	distributedTracingOpts *tracing.Options
}

func (h *hndl) GetManager() *plugins.Manager {
	return h.manager
}

func (h *hndl) SetManager(m *plugins.Manager) error {
	extraRoute(m, "/v1/batch/data/{path:.+}", PrometheusHandle, h.ServeHTTP)
	extraRoute(m, "/v1/batch/data", PrometheusHandle, h.ServeHTTP)
	if pr := m.PrometheusRegister(); pr != nil {
		if err := pr.Register(h.counterPEQCache); err != nil {
			return err
		}
	}
	// Useful in Fallback mode, but otherwise we pull the logger from the
	// runtime instance.
	if h.Logger == nil {
		h.Logger = m.Logger()
	}

	h.dl = logs.Lookup(m)
	if h.dl != nil {
		h.decisionIDFactory = defaultDecisionIDFactory
	}
	h.manager = m
	return nil
}

func (h *hndl) GetDistributedTracingOpts() *tracing.Options {
	return h.distributedTracingOpts
}

// WithDistributedTracingOptions sets the tracing options used by the server.
func (h *hndl) WithDistributedTracingOptions(options *tracing.Options) BatchQueryHandler {
	h.distributedTracingOpts = options
	return h
}

// WithDecisionLoggerWithErr sets the decision logger used by the server.
func (h *hndl) WithDecisionLogger(logger func(context.Context, *server.Info) error) BatchQueryHandler {
	h.decisionLoggerFunc = logger
	return h
}

// WithDecisionIDFactory sets a function on the server to generate decision IDs.
// Used primarily for testing.
func (h *hndl) WithDecisionIDFactory(f func() string) BatchQueryHandler {
	h.decisionIDFactory = f
	return h
}

// Note(philip): This function will increase cache pressure on the query caching
// system, which is why I bumped the entries limit.

// Note(philip): Because metrics.Metrics cannot share/edit/inherit from other
// metrics objects, we have a weird situation here where the individual eval
// metrics may have zeroed fields, because the only valid measure exists at
// global scope only.

// Note(philip): We have an "outer" metrics object here that tracks the
// overall metrics for the batch request as a whole. Individual queries have
// their own separate per-query metrics objects, which are reported on each
// query's results object.
func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// func (s *Server) v1BatchDataPost(w http.ResponseWriter, r *http.Request) {

	m := metrics.New()
	m.Timer(metrics.ServerHandler).Start()

	batchDecisionID := h.generateDecisionID()
	ctx := logging.WithBatchDecisionID(r.Context(), batchDecisionID)
	annotateSpan(ctx, batchDecisionID)

	vars := mux.Vars(r)
	urlPath := vars["path"]
	explainMode := getExplain(r.URL.Query()[types.ParamExplainV1], types.ExplainOffV1)
	includeInstrumentation := getBoolParam(r.URL, types.ParamInstrumentV1, true)
	provenance := getBoolParam(r.URL, types.ParamProvenanceV1, true)
	strictBuiltinErrors := getBoolParam(r.URL, types.ParamStrictBuiltinErrors, true)
	includeMetrics := includeMetrics(r)
	formatPretty := pretty(r)

	m.Timer(metrics.RegoInputParse).Start()

	inputs, goInputs, err := readInputBatchPostV1(r)
	if err != nil {
		writer.ErrorString(w, http.StatusBadRequest, types.CodeInvalidParameter, err)
		return
	}

	m.Timer(metrics.RegoInputParse).Stop()

	// ----------------------------------------------------------------------------------------------------------------------

	// The sorted keys list allows us to drive both deterministic Decision ID
	// generation, and also deterministic write target indices for the worker
	// goroutines, later in this function.
	sortedKeys := make([]string, 0, len(inputs))
	for k := range inputs {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	// Write targets for the goroutines. Sometimes we'll have some wasted
	// memory, but it'll be reclaimed quickly in most cases.
	results := make([]*DataResponseWithHTTPCodeV1, len(sortedKeys))
	errs := make([]*ErrorResponseWithHTTPCodeV1, len(sortedKeys))

	// Note(philip): While queries may complete in arbitrary order, their
	// decision IDs are generated in a deterministic order, based on the sorted
	// order of the keys used to ID each unique query. This makes testing
	// easier, since the decision IDs in the results will be deterministic.
	queryDecisionIDs := make(map[string]string, len(inputs))
	for _, k := range sortedKeys {
		queryDecisionIDs[k] = h.generateDecisionID()
	}

	var preparedQuery *rego.PreparedEvalQuery
	{
		txn, err := h.manager.Store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
		if err != nil {
			writer.ErrorAuto(w, err)
			return
		}
		defer h.manager.Store.Abort(ctx, txn)
		if preparedQuery, err = h.ensurePreparedEvalQueryIsCached(ctx, txn, &evalSingleParams{
			URLPath:                urlPath,
			ExplainMode:            explainMode,
			IncludeInstrumentation: includeInstrumentation,
			Provenance:             provenance,
			StrictBuiltinErrors:    strictBuiltinErrors,
			M:                      m,
			IncludeMetrics:         includeMetrics,
			Pretty:                 formatPretty,
		}); err != nil {
			writer.ErrorAuto(w, err)
			return
		}
	}

	// Start a limited-size worker pool to avoid goroutine startup/sync overheads.
	// Workers pull jobs from the input slice until they run out of work to do.
	var wg sync.WaitGroup
	maxProcs := runtime.GOMAXPROCS(0)
	poolCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Cause(poolCtx)) // Needed to prevent possible context leak.
	wg.Add(maxProcs)
	{
		// We launch all of the queries in parallel, and fill out the appropriate
		// indices in the write target slices. The writes look a little funky
		// because we want the write target slices to have pointers-to-structs, for
		// easy nil-checking later on.
		for i := range maxProcs {
			go func() {
				canceller := topdown.NewCancel()
				exit := make(chan struct{})
				defer close(exit)
				go waitForDone(ctx, exit, func() {
					canceller.Cancel()
				})
				// We use a strided access pattern: each goroutine works on
				// every Nth work item, resulting in a roughly even distribution
				// of jobs across workers.
				for idx := i; idx < len(sortedKeys); idx += maxProcs {
					k := sortedKeys[idx]
					v := inputs[k]
					perQueryMetrics := metrics.New()
					rDest := &(results[idx])
					eDest := &(errs[idx])
					// Note(philip): We rely here on concurrent, read-only map accesses
					// being safe.
					decisionID := queryDecisionIDs[k]

					ctx := logging.WithDecisionID(r.Context(), decisionID)
					ctx = logging.WithBatchDecisionID(ctx, batchDecisionID)
					annotateSpan(ctx, decisionID)

					perQueryMetrics.Timer(metrics.ServerHandler).Start()
					defer perQueryMetrics.Timer(metrics.ServerHandler).Stop()

					select {
					case <-poolCtx.Done():
						*eDest = &ErrorResponseWithHTTPCodeV1{
							DecisionID:     decisionID,
							HTTPStatusCode: "500",
							ErrorV1: types.ErrorV1{
								Code:    types.CodeInternal,
								Message: poolCtx.Err().Error(),
							},
						}
						cancel(err)
						continue
					default:
						// Create a new read transaction on the store.
						txn, err := h.manager.Store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
						if err != nil {
							*eDest = &ErrorResponseWithHTTPCodeV1{
								DecisionID:     decisionID,
								HTTPStatusCode: "500",
								ErrorV1: types.ErrorV1{
									Code:    types.CodeInternal,
									Message: err.Error(),
								},
							}
							continue
						}
						defer h.manager.Store.Abort(ctx, txn)

						// Create global metric if it doesn't already exist.
						m.Counter(metrics.ServerQueryCacheHit)

						// Build up parameters for the eval.
						params := evalSingleParams{
							DecisionID:             decisionID,
							URLPath:                urlPath,
							ExplainMode:            explainMode,
							IncludeInstrumentation: includeInstrumentation,
							Provenance:             provenance,
							StrictBuiltinErrors:    strictBuiltinErrors,
							GoInput:                goInputs[k],
							M:                      perQueryMetrics,
							IncludeMetrics:         includeMetrics,
							Pretty:                 formatPretty,
							ExternalCancel:         canceller,
						}

						result, err := h.evalInputSinglePreparedQuery(ctx, txn, v, &params, preparedQuery)
						if err != nil {
							*eDest = &ErrorResponseWithHTTPCodeV1{
								DecisionID:     decisionID,
								HTTPStatusCode: "500",
								ErrorV1: types.ErrorV1{
									Code:    types.CodeInternal,
									Message: err.Error(),
								},
							}
							continue
						}

						// Unwrap cache hit counter, then add hits metric to global metric.
						if cacheHit, ok := (*result).Metrics[metrics.ServerQueryCacheHit]; ok {
							if counter, ok := cacheHit.(metrics.Counter); ok {
								if counterValue, ok := counter.Value().(uint64); ok && counterValue > 0 {
									m.Counter(metrics.ServerQueryCacheHit).Incr()
								}
							}
						}
						*rDest = &DataResponseWithHTTPCodeV1{
							HTTPStatusCode: "200",
							DataResponseV1: *result,
						}
					}
				}
				// When we're done processing all items for this goroutine, bump the counter.
				wg.Done()
			}()
		}
	}

	// Note(philip): This block *should* never trigger unless something is
	// horrifically amiss at the server-wide level.
	wg.Wait()
	if pctxErr := context.Cause(poolCtx); pctxErr != poolCtx.Err() {
		writer.ErrorAuto(w, pctxErr)
		return
	}

	// Populate maps with non-nil results.
	errMap := map[string]ErrorResponseWithHTTPCodeV1{}
	resultMap := map[string]DataResponseWithHTTPCodeV1{}
	for i := range results {
		if errs[i] != nil {
			errMap[sortedKeys[i]] = *errs[i]
		}
		if results[i] != nil {
			resultMap[sortedKeys[i]] = *results[i]
		}
	}

	// Golang is silly, and forces us to collate everything together at the end.
	responses := make(map[string]BatchDataRespType, len(inputs))
	for k, v := range resultMap {
		if len(errMap) == 0 {
			v.HTTPStatusCode = ""
		}
		responses[k] = v
	}
	for k, v := range errMap {
		if len(resultMap) == 0 {
			v.HTTPStatusCode = ""
		}
		responses[k] = v
	}

	m.Timer(metrics.ServerHandler).Stop()

	output := BatchDataResponseV1{
		BatchDecisionID: batchDecisionID,
		Responses:       responses,
	}

	if inputs == nil {
		output.Warning = types.NewWarning(types.CodeAPIUsageWarn, MsgInputsKeyMissing)
	}

	if includeMetrics {
		output.Metrics = m.All()
	}

	// Handle the response after all evals completed.
	switch {
	// All succeeded.
	case len(errMap) == 0 && len(resultMap) > 0:
		writer.JSONOK(w, output, formatPretty)
	// Some succeeded, some failed.
	case len(errMap) > 0 && len(resultMap) > 0:
		writer.JSON(w, 207, output, formatPretty)
	// All failed.
	case len(errMap) > 0 && len(resultMap) == 0:
		writer.JSON(w, 500, output, formatPretty)
	// No inputs / all other cases:
	default:
		writer.JSON(w, 200, output, formatPretty)
	}
}

type evalSingleParams struct {
	DecisionID             string
	URLPath                string
	ExplainMode            types.ExplainModeV1
	IncludeInstrumentation bool
	Provenance             bool
	StrictBuiltinErrors    bool
	GoInput                *any
	M                      metrics.Metrics
	IncludeMetrics         bool
	Pretty                 bool
	ExternalCancel         topdown.Cancel
}

// Borrowed wholesale from `rego/rego.go`:
func waitForDone(ctx context.Context, exit chan struct{}, f func()) {
	select {
	case <-exit:
		return
	case <-ctx.Done():
		f()
		return
	}
}

// Fetch the PreparedEvalQuery for a query batch from the cache if possible. If
// the prepared query isn't in the cache, it is rebuilt from scratch, and
// inserted into the cache.
func (h *hndl) ensurePreparedEvalQueryIsCached(ctx context.Context, txn storage.Transaction, params *evalSingleParams) (*rego.PreparedEvalQuery, error) {
	legacyRevision, br, err := getRevisions(ctx, h.manager.Store, txn)
	if err != nil {
		return nil, err
	}

	var ndbCache builtins.NDBCache
	// if s.ndbCacheEnabled {
	// 	ndbCache = builtins.NDBCache{}
	// }

	ctx, logger := h.getDecisionLogger(ctx, legacyRevision, br)

	var buf *topdown.BufferTracer

	if params.ExplainMode != types.ExplainOffV1 {
		buf = topdown.NewBufferTracer()
	}

	pqID := "v1BatchDataPost::"
	if params.StrictBuiltinErrors {
		pqID += "strict-builtin-errors::"
	}
	pqID += params.URLPath
	preparedQuery, ok := h.getCachedPreparedEvalQuery(pqID, params.M)
	if !ok {
		opts := []func(*rego.Rego){
			rego.Compiler(h.manager.GetCompiler()),
			rego.Store(h.manager.Store),
		}

		// Set resolvers on the base Rego object to avoid having them get
		// re-initialized, and to propagate them to the prepared query.
		for _, r := range h.manager.GetWasmResolvers() {
			for _, entrypoint := range r.Entrypoints() {
				opts = append(opts, rego.Resolver(entrypoint, r))
			}
		}

		rego, err := h.makeRego(ctx, params.StrictBuiltinErrors, txn, nil, params.URLPath, params.M, params.IncludeInstrumentation, buf, opts)
		if err != nil {
			// dlog?
			_ = logger.Log(ctx, txn, params.URLPath, "", nil, nil, nil, ndbCache, err, params.M)
			return nil, err
		}

		pq, err := rego.PrepareForEval(ctx)
		if err != nil {
			_ = logger.Log(ctx, txn, params.URLPath, "", nil, nil, nil, ndbCache, err, params.M)
			return nil, err
		}
		preparedQuery = &pq
		h.preparedEvalQueryCache.Add(pqID, preparedQuery)
	}
	return preparedQuery, nil
}

// Takes a prepared query, and runs it through eval. Meant to be used with the
// Batch Query API, but may have other uses.
func (h *hndl) evalInputSinglePreparedQuery(ctx context.Context, txn storage.Transaction, input ast.Value, params *evalSingleParams, pq *rego.PreparedEvalQuery) (*types.DataResponseV1, error) {
	legacyRevision, br, err := getRevisions(ctx, h.manager.Store, txn)
	if err != nil {
		return nil, err
	}

	ctx, logger := h.getDecisionLogger(ctx, legacyRevision, br)

	var ndbCache builtins.NDBCache
	// if s.ndbCacheEnabled {
	// 	ndbCache = builtins.NDBCache{}
	// }

	var buf *topdown.BufferTracer

	if params.ExplainMode != types.ExplainOffV1 {
		buf = topdown.NewBufferTracer()
	}

	// We skip over the "insert if not in cache" logic from the original eval
	// logic, because we have the prepared query as a method parameter.
	preparedQuery := pq

	evalOpts := []rego.EvalOption{
		rego.EvalTransaction(txn),
		rego.EvalParsedInput(input),
		rego.EvalMetrics(params.M),
		rego.EvalQueryTracer(buf),
		// rego.EvalInterQueryBuiltinCache(s.interQueryBuiltinCache),
		rego.EvalInstrument(params.IncludeInstrumentation),
		rego.EvalNDBuiltinCache(ndbCache),
		rego.EvalExternalCancel(params.ExternalCancel),
	}

	rs, err := preparedQuery.Eval(
		ctx,
		evalOpts...,
	)

	params.M.Timer(metrics.ServerHandler).Stop()

	// Handle results.
	if err != nil {
		_ = logger.Log(ctx, txn, params.URLPath, "", params.GoInput, input, nil, ndbCache, err, params.M)
		return nil, err
	}

	result := types.DataResponseV1{
		DecisionID: params.DecisionID,
	}

	if input == nil {
		result.Warning = types.NewWarning(types.CodeAPIUsageWarn, types.MsgInputKeyMissing)
	}

	if params.IncludeMetrics || params.IncludeInstrumentation {
		result.Metrics = params.M.All()
	}

	if params.Provenance {
		result.Provenance = h.getProvenance(legacyRevision, br)
	}

	if len(rs) == 0 {
		if params.ExplainMode == types.ExplainFullV1 {
			result.Explanation, err = types.NewTraceV1(lineage.Full(*buf), params.Pretty)
			if err != nil {
				return nil, err
			}
		}
		if err = logger.Log(ctx, txn, params.URLPath, "", params.GoInput, input, result.Result, ndbCache, nil, params.M); err != nil {
			return nil, err
		}
		return &result, nil
	}

	// Otherwise, we have at least one result in the ResultSet.
	result.Result = &rs[0].Expressions[0].Value

	if params.ExplainMode != types.ExplainOffV1 {
		result.Explanation = h.getExplainResponse(params.ExplainMode, *buf, params.Pretty)
	}

	if err := logger.Log(ctx, txn, params.URLPath, "", params.GoInput, input, result.Result, ndbCache, nil, params.M); err != nil {
		return nil, err
	}

	return &result, nil
}

// taken from v1/server/server.go
func (h *hndl) getCachedPreparedEvalQuery(key string, m metrics.Metrics) (*rego.PreparedEvalQuery, bool) {
	pq, ok := h.preparedEvalQueryCache.Get(key)
	m.Counter(metrics.ServerQueryCacheHit) // Creates the counter on the metrics if it doesn't exist, starts at 0
	if ok {
		m.Counter(metrics.ServerQueryCacheHit).Incr() // Increment counter on hit
		h.counterPEQCache.WithLabelValues("hit").Inc()
		return pq, true
	}
	h.counterPEQCache.WithLabelValues("miss").Inc()
	return nil, false
}

func (h *hndl) makeRego(_ context.Context,
	strictBuiltinErrors bool,
	txn storage.Transaction,
	input ast.Value,
	urlPath string,
	m metrics.Metrics,
	instrument bool,
	tracer topdown.QueryTracer,
	opts []func(*rego.Rego),
) (*rego.Rego, error) {
	queryPath := stringPathToDataRef(urlPath).String()

	opts = append(
		opts,
		rego.Transaction(txn),
		rego.Query(queryPath),
		rego.ParsedInput(input),
		rego.Metrics(m),
		rego.QueryTracer(tracer),
		rego.Instrument(instrument),
		rego.Runtime(h.manager.Info),
		rego.UnsafeBuiltins(unsafeBuiltinsMap),
		rego.StrictBuiltinErrors(strictBuiltinErrors),
		rego.PrintHook(h.manager.PrintHook()),
		rego.QueryTracer(tracer),
	)

	if h.distributedTracingOpts != nil {
		opts = append(opts, rego.DistributedTracingOpts(*h.distributedTracingOpts))
	}

	return rego.New(opts...), nil
}

func annotateSpan(ctx context.Context, decisionID string) {
	if decisionID == "" {
		return
	}
	trace.SpanFromContext(ctx).
		SetAttributes(attribute.String(otelDecisionIDAttr, decisionID))
}

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

func (*hndl) getExplainResponse(explainMode types.ExplainModeV1, trace []*topdown.Event, pretty bool) (explanation types.TraceV1) {
	switch explainMode {
	case types.ExplainNotesV1:
		var err error
		explanation, err = types.NewTraceV1(lineage.Notes(trace), pretty)
		if err != nil {
			break
		}
	case types.ExplainFailsV1:
		var err error
		explanation, err = types.NewTraceV1(lineage.Fails(trace), pretty)
		if err != nil {
			break
		}
	case types.ExplainFullV1:
		var err error
		explanation, err = types.NewTraceV1(lineage.Full(trace), pretty)
		if err != nil {
			break
		}
	case types.ExplainDebugV1:
		var err error
		explanation, err = types.NewTraceV1(lineage.Debug(trace), pretty)
		if err != nil {
			break
		}
	}
	return explanation
}

func (h *hndl) getProvenance(legacyRevision string, revisions map[string]server.BundleInfo) *types.ProvenanceV1 {
	p := &types.ProvenanceV1{
		Version:   version.Version,
		Vcs:       version.Vcs,
		Timestamp: version.Timestamp,
		Hostname:  version.Hostname,
	}

	// For backwards compatibility, if the bundles are using the old
	// style config we need to fill in the older `Revision` field.
	// Otherwise use the newer `Bundles` keyword.
	if h.hasLegacyBundle(legacyRevision, revisions) {
		p.Revision = legacyRevision
	} else {
		p.Bundles = map[string]types.ProvenanceBundleV1{}
		for name, revision := range revisions {
			p.Bundles[name] = types.ProvenanceBundleV1{Revision: revision.Revision}
		}
	}

	return p
}

func (h *hndl) hasLegacyBundle(legacyRevision string, _ map[string]server.BundleInfo) bool {
	bp := bundlePlugin.Lookup(h.manager)
	return legacyRevision != "" || (bp != nil && !bp.Config().IsMultiBundle())
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

func (h *hndl) getDecisionLogger(ctx context.Context, legacyRevision string, revisions map[string]server.BundleInfo) (context.Context, decisionLogger) {
	// if intermediateResultsEnabled {
	// 	ctx = context.WithValue(ctx, IntermediateResultsContextKey{}, make(map[string]interface{}))
	// }
	var logger decisionLogger

	// For backwards compatibility use `revision` as needed.
	if h.hasLegacyBundle(legacyRevision, revisions) {
		logger.revision = legacyRevision
	} else {
		logger.revisions = revisions
	}
	if h.decisionLoggerFunc != nil {
		logger.logger = h.decisionLoggerFunc
	} else if h.dl != nil {
		logger.logger = h.dl.Log
	}

	return ctx, logger
}
