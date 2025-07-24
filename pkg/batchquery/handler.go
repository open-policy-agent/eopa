package batchquery

import (
	"context"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
	WithLicensedMode(bool) BatchQueryHandler
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
	licensedMode bool
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
	m.ExtraRoute("POST /v1/batch/data/{path...}", PrometheusHandle, h.ServeHTTP)
	m.ExtraRoute("POST /v1/batch/data", PrometheusHandle, h.ServeHTTP)
	m.ExtraAuthorizerRoute(func(method string, path []any) bool {
		s0 := path[0].(string)
		s1 := path[1].(string)
		s2 := path[2].(string)
		return method == "POST" && s0 == "v1" && s1 == "batch" && s2 == "data"
	})
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

func (h *hndl) SetLicensedMode(licensed bool) {
	h.licensedMode = licensed
}

func (h *hndl) WithLicensedMode(licensed bool) BatchQueryHandler {
	h.licensedMode = licensed
	return h
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

// Note(philip): Because metrics.Metrics cannot share/edit/inherit from other
// metrics objects, we have a weird situation here where the individual eval
// metrics may have zeroed fields, because the only valid measure exists at
// global scope only.

// Note(philip): We have an "outer" metrics object here that tracks the
// overall metrics for the batch request as a whole. Individual queries have
// their own separate per-query metrics objects, which are reported on each
// query's results object.
func (h *hndl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m := metrics.New()
	m.Timer(metrics.ServerHandler).Start()

	batchDecisionID := h.generateDecisionID()
	ctx := logging.WithBatchDecisionID(r.Context(), batchDecisionID)
	annotateSpan(ctx, batchDecisionID)

	urlPath := r.PathValue("path")
	explainMode := getExplain(r.URL.Query()[types.ParamExplainV1], types.ExplainOffV1)
	includeInstrumentation := getBoolParam(r.URL, types.ParamInstrumentV1, true)
	provenance := getBoolParam(r.URL, types.ParamProvenanceV1, true)
	strictBuiltinErrors := getBoolParam(r.URL, types.ParamStrictBuiltinErrors, true)
	includeMetrics := includeMetrics(r)
	formatPretty := pretty(r)

	m.Timer(metrics.RegoInputParse).Start()

	maxProcs := runtime.GOMAXPROCS(0)
	// Extract slice-of-slices for keys and values from the input. Each
	// sub-slice represents a chunk of work for one of the worker goroutines in
	// the worker pool.
	commonInputIf, keys, ifValues, err := readInputBatchPostV1(r, maxProcs)
	if err != nil {
		writer.ErrorString(w, http.StatusBadRequest, types.CodeInvalidParameter, err)
		return
	}

	m.Timer(metrics.RegoInputParse).Stop()

	numInputs := 0
	for _, ks := range keys {
		numInputs += len(ks)
	}
	output := BatchDataResponseV1{
		BatchDecisionID: batchDecisionID,
		Responses:       make(map[string]BatchDataRespType, numInputs), // Prealloc destination.
	}

	// Setup work for the worker pool that should be done at "global" scope.
	var preparedQuery *rego.PreparedEvalQuery
	var globalSharedEvalParams evalSingleParams
	{
		txn, err := h.manager.Store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
		if err != nil {
			writer.ErrorAuto(w, err)
			return
		}
		defer h.manager.Store.Abort(ctx, txn)

		// Initialize the eval params we'll be using later.
		legacyRevision, br, err := getRevisions(ctx, h.manager.Store, txn)
		if err != nil {
			writer.ErrorAuto(w, err)
			return
		}
		logger := h.getDecisionLogger(legacyRevision, br)

		globalSharedEvalParams = evalSingleParams{
			URLPath:                urlPath,
			ExplainMode:            explainMode,
			IncludeInstrumentation: includeInstrumentation,
			Provenance:             provenance,
			StrictBuiltinErrors:    strictBuiltinErrors,
			M:                      m,
			IncludeMetrics:         includeMetrics,
			Pretty:                 formatPretty,
			LegacyRevision:         legacyRevision,
			BundleRevisions:        br,
			DecisionLogger:         logger,
		}
		if preparedQuery, err = h.ensurePreparedEvalQueryIsCached(ctx, txn, &globalSharedEvalParams); err != nil {
			writer.ErrorAuto(w, err)
			return
		}
	}

	// -----------------------------------------------------------------------
	// Spawn work group, do the work, and fill out the results.

	// Workers each get a slice of input values to work with. These will match
	// up later with slices of keys for reassembling into the output.
	responseChunks := make([][]BatchDataRespType, len(keys))
	var resultsCounter, errCounter atomic.Int32

	// Start a limited-size worker pool to avoid goroutine startup/sync overheads.
	// Workers pull inputs from their slice until they run out of work to do.
	var wg sync.WaitGroup
	poolCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Cause(poolCtx)) // Needed to prevent possible context leak.

	for i, vs := range ifValues {
		wg.Add(1)
		go func(values []any, index int) {
			canceller := topdown.NewCancel()
			exit := make(chan struct{})
			defer close(exit)
			go waitForDone(ctx, exit, func() {
				canceller.Cancel()
			})
			resultCount, errCount := int32(0), int32(0)
			outputs := make([]BatchDataRespType, 0, len(values))
			// Copy global params, then update in the loop for each eval.
			params := globalSharedEvalParams

			// The job loop for each worker:
			for _, v := range values {
				perQueryMetrics := metrics.New()
				decisionID := h.generateDecisionID()

				// Update params for this eval.
				params.DecisionID = decisionID
				params.M = perQueryMetrics
				params.ExternalCancel = canceller

				ctx := logging.WithDecisionID(r.Context(), decisionID)
				ctx = logging.WithBatchDecisionID(ctx, batchDecisionID)
				annotateSpan(ctx, decisionID)

				// Note(philip): We can't simply `defer` the timer stop
				// call, or else they'll all keep running until the
				// goroutine function exits! This led to weirdly increasing
				// individual eval timer stats in older versions of the
				// Batch Query API.
				perQueryMetrics.Timer(metrics.ServerHandler).Start()

				select {
				case <-poolCtx.Done():
					outputs = append(outputs, &ErrorResponseWithHTTPCodeV1{
						DecisionID: decisionID,
						ErrorV1: types.ErrorV1{
							Code:    types.CodeInternal,
							Message: poolCtx.Err().Error(),
						},
					})
					perQueryMetrics.Timer(metrics.ServerHandler).Stop()
					errCount++
					cancel(err) // Note(philip): This is the only time cancel should be called inside the goroutine.
					continue
				default:
					// Create a new read transaction on the store.
					// Note(philip): We can't defer the transaction aborts, or
					// else they may stack up until the goroutine function exits.
					txn, err := h.manager.Store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
					if err != nil {
						outputs = append(outputs, &ErrorResponseWithHTTPCodeV1{
							DecisionID: decisionID,
							ErrorV1: types.ErrorV1{
								Code:    types.CodeInternal,
								Message: err.Error(),
							},
						})
						perQueryMetrics.Timer(metrics.ServerHandler).Stop()
						errCount++
						continue
					}

					// Create global metric if it doesn't already exist.
					m.Counter(metrics.ServerQueryCacheHit)

					// Only merge if both common_input and the input are objects.
					inputIf := v
					if mCommon, ok := asMap(commonInputIf); ok {
						if mInput, ok := asMap(v); ok {
							inputIf = mergeMaps(mCommon, mInput)
						}
					}
					params.GoInput = &inputIf
					// Only bother with ast.Value conversion if we're in fallback mode.
					if !h.licensedMode {
						inputASTValue, err := ast.InterfaceToValue(inputIf)
						if err != nil {
							outputs = append(outputs, &ErrorResponseWithHTTPCodeV1{
								DecisionID: decisionID,
								ErrorV1: types.ErrorV1{
									Code:    types.CodeInternal,
									Message: err.Error(),
								},
							})
							h.manager.Store.Abort(ctx, txn)
							perQueryMetrics.Timer(metrics.ServerHandler).Stop()
							errCount++
							continue
						}
						params.ASTInput = inputASTValue
					}
					result, err := h.evalInputSinglePreparedQuery(ctx, txn, &params, preparedQuery)
					if err != nil {
						outputs = append(outputs, &ErrorResponseWithHTTPCodeV1{
							DecisionID: decisionID,
							ErrorV1: types.ErrorV1{
								Code:    types.CodeInternal,
								Message: err.Error(),
							},
						})
						h.manager.Store.Abort(ctx, txn)
						perQueryMetrics.Timer(metrics.ServerHandler).Stop()
						errCount++
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
					outputs = append(outputs, &DataResponseWithHTTPCodeV1{
						DataResponseV1: *result,
					})
					h.manager.Store.Abort(ctx, txn)
					perQueryMetrics.Timer(metrics.ServerHandler).Stop()
					resultCount++
				}
			}
			// Insert results into the output chunks slice.
			// Note(philip): Because each goroutine gets a disjoint index, we shouldn't need locking here.
			responseChunks[index] = outputs
			resultsCounter.Add(resultCount)
			errCounter.Add(errCount)
			// When we're done processing all items for this goroutine, bump the workgroup's counter.
			wg.Done()
		}(vs, i)
	}

	// Note(philip): This block *should* never trigger unless something is
	// horrifically amiss at the server-wide level.
	wg.Wait()
	if pctxErr := context.Cause(poolCtx); pctxErr != poolCtx.Err() {
		writer.ErrorAuto(w, pctxErr)
		return
	}

	// -----------------------------------------------------------------------
	// Reassemble the unordered results into the final destination map.

	resultsCount := resultsCounter.Load()
	errCount := errCounter.Load()

	// Note(philip): I originally wanted to use iterators here, but
	// range-over-func still seems to generate extra allocs, as of Go 1.24.
	for i := range keys {
		ks, vs := keys[i], responseChunks[i]
		for j := range ks {
			k, v := ks[j], vs[j]
			// Only add status codes in the "Mixed Results" case.
			if errCount != 0 && resultsCount != 0 {
				switch r := v.(type) {
				case *DataResponseWithHTTPCodeV1:
					r.HTTPStatusCode = "200"
				case *ErrorResponseWithHTTPCodeV1:
					r.HTTPStatusCode = "500"
				}
			}
			output.Responses[k] = v
		}
	}

	m.Timer(metrics.ServerHandler).Stop()

	if numInputs == 0 && keys == nil {
		output.Warning = types.NewWarning(types.CodeAPIUsageWarn, MsgInputsKeyMissing)
	}

	if includeMetrics {
		output.Metrics = m.All()
	}

	// Handle the response after all evals completed.
	switch {
	// All succeeded.
	case errCount == 0 && resultsCount > 0:
		writer.JSONOK(w, output, formatPretty)
	// Some succeeded, some failed.
	case errCount > 0 && resultsCount > 0:
		writer.JSON(w, 207, output, formatPretty)
	// All failed.
	case errCount > 0 && resultsCount == 0:
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
	IncludeMetrics         bool
	Pretty                 bool
	ASTInput               ast.Value
	GoInput                *any
	M                      metrics.Metrics
	ExternalCancel         topdown.Cancel
	LegacyRevision         string
	BundleRevisions        map[string]server.BundleInfo
	DecisionLogger         decisionLogger
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
	// var ndbCache builtins.NDBCache
	// if s.ndbCacheEnabled {
	// 	ndbCache = builtins.NDBCache{}
	// }

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
			_ = params.DecisionLogger.Log(ctx, txn, params.URLPath, "", nil, nil, nil, nil, err, params.M)
			return nil, err
		}

		pq, err := rego.PrepareForEval(ctx)
		if err != nil {
			_ = params.DecisionLogger.Log(ctx, txn, params.URLPath, "", nil, nil, nil, nil, err, params.M)
			return nil, err
		}
		preparedQuery = &pq
		h.preparedEvalQueryCache.Add(pqID, preparedQuery)
	}
	return preparedQuery, nil
}

// Takes a prepared query, and runs it through eval. Meant to be used with the
// Batch Query API, but may have other uses.
func (h *hndl) evalInputSinglePreparedQuery(ctx context.Context, txn storage.Transaction, params *evalSingleParams, pq *rego.PreparedEvalQuery) (*types.DataResponseV1, error) {
	// var ndbCache builtins.NDBCache
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

	evalOpts := make([]rego.EvalOption, 0, 7)
	evalOpts = append(evalOpts, []rego.EvalOption{
		rego.EvalTransaction(txn),
		rego.EvalMetrics(params.M),
		rego.EvalQueryTracer(buf),
		// rego.EvalInterQueryBuiltinCache(s.interQueryBuiltinCache),
		rego.EvalInstrument(params.IncludeInstrumentation),
		// rego.EvalNDBuiltinCache(ndbCache),
		rego.EvalExternalCancel(params.ExternalCancel),
	}...)

	// Only feed in the AST representation if it's provided.
	if params.ASTInput != nil {
		evalOpts = append(evalOpts, rego.EvalParsedInput(params.ASTInput))
	} else {
		// Unwrap the *any if it's non-nil.
		var input any
		input = params.GoInput
		if params.GoInput != nil {
			input = *params.GoInput
		}
		evalOpts = append(evalOpts, rego.EvalInput(input))
	}

	rs, err := preparedQuery.Eval(
		ctx,
		evalOpts...,
	)

	// OPA's metrics use restartable timers. We stop this timer to accumulate
	// the time passed so far, then restart it to allow the caller to accurately
	// track overall time passing for the handler. This means decision logs will
	// report time spent up to the end of evaluation, but metrics for the
	// request as a whole will tell a more holistic story for each eval.
	params.M.Timer(metrics.ServerHandler).Stop()
	params.M.Timer(metrics.ServerHandler).Start()

	// Handle results.
	if err != nil {
		_ = params.DecisionLogger.Log(ctx, txn, params.URLPath, "", params.GoInput, params.ASTInput, nil, nil, err, params.M)
		return nil, err
	}

	result := types.DataResponseV1{
		DecisionID: params.DecisionID,
	}

	if params.ASTInput == nil && params.GoInput == nil {
		result.Warning = types.NewWarning(types.CodeAPIUsageWarn, types.MsgInputKeyMissing)
	}

	if params.IncludeMetrics || params.IncludeInstrumentation {
		result.Metrics = params.M.All()
	}

	if params.Provenance {
		result.Provenance = h.getProvenance(params.LegacyRevision, params.BundleRevisions)
	}

	if len(rs) == 0 {
		if params.ExplainMode == types.ExplainFullV1 {
			result.Explanation, err = types.NewTraceV1(lineage.Full(*buf), params.Pretty)
			if err != nil {
				return nil, err
			}
		}
		if err = params.DecisionLogger.Log(ctx, txn, params.URLPath, "", params.GoInput, params.ASTInput, result.Result, nil, nil, params.M); err != nil {
			return nil, err
		}
		return &result, nil
	}

	// Otherwise, we have at least one result in the ResultSet.
	result.Result = &rs[0].Expressions[0].Value

	if params.ExplainMode != types.ExplainOffV1 {
		result.Explanation = h.getExplainResponse(params.ExplainMode, *buf, params.Pretty)
	}

	if err := params.DecisionLogger.Log(ctx, txn, params.URLPath, "", params.GoInput, params.ASTInput, result.Result, nil, nil, params.M); err != nil {
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
	p := strings.SplitSeq(s, "/")
	for x := range p {
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

// Note(philip): This used to take a context.Context parameter, for intermediate
// results. Since that code path was removed, I've also trimmed out the extra
// parameter.
func (h *hndl) getDecisionLogger(legacyRevision string, revisions map[string]server.BundleInfo) decisionLogger {
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

	return logger
}
