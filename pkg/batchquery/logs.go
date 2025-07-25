package batchquery

import (
	"context"
	"fmt"
	"maps"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/server"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle"
)

// decisionLogType is injected under custom.type in the DL entry
const decisionLogType = "eopa.styra.com/batch"

type decisionLogger struct {
	revisions map[string]server.BundleInfo
	logger    func(context.Context, *server.Info) error
	revision  string // Deprecated: Use `revisions` instead.
}

func (l decisionLogger) Log(ctx context.Context, txn storage.Transaction, path string, query string, goInput *interface{}, astInput ast.Value, goResults *interface{}, ndbCache builtins.NDBCache, err error, m metrics.Metrics) error {
	bundles := maps.Clone(l.revisions)

	rctx := logging.RequestContext{}
	if r, ok := logging.FromContext(ctx); ok {
		rctx = *r
	}
	decisionID, _ := logging.DecisionIDFromContext(ctx)
	batchDecisionID, batchPresent := logging.BatchDecisionIDFromContext(ctx)

	var httpRctx logging.HTTPRequestContext

	httpRctxVal, _ := logging.HTTPRequestContextFromContext(ctx)
	if httpRctxVal != nil {
		httpRctx = *httpRctxVal
	}

	info := &server.Info{
		Txn:                txn,
		Revision:           l.revision,
		Bundles:            bundles,
		Timestamp:          time.Now().UTC(),
		DecisionID:         decisionID,
		RemoteAddr:         rctx.ClientAddr,
		HTTPRequestContext: httpRctx,
		Path:               path,
		Query:              query,
		Input:              goInput,
		InputAST:           astInput,
		Results:            goResults,
		Error:              err,
		Metrics:            m,
		RequestID:          rctx.ReqID,
		Custom: map[string]any{
			"type": decisionLogType,
		}}

	if ndbCache != nil {
		x, err := ast.JSON(ndbCache.AsValue())
		if err != nil {
			return err
		}
		info.NDBuiltinCache = &x
	}

	if batchPresent {
		info.BatchDecisionID = batchDecisionID
	}

	sctx := trace.SpanFromContext(ctx).SpanContext()
	if sctx.IsValid() {
		info.TraceID = sctx.TraceID().String()
		info.SpanID = sctx.SpanID().String()
	}

	// if intermediateResultsEnabled {
	// 	if iresults, ok := ctx.Value(server.IntermediateResultsContextKey{}).(map[string]interface{}); ok {
	// 		info.IntermediateResults = iresults
	// 	}
	// }

	if l.logger != nil {
		if err := l.logger(ctx, info); err != nil {
			return fmt.Errorf("decision_logs: %w", err)
		}
	}

	return nil
}

func getRevisions(ctx context.Context, store storage.Store, txn storage.Transaction) (string, map[string]server.BundleInfo, error) {
	// Check if we still have a legacy bundle manifest in the store
	legacyRevision, err := bundle.LegacyReadRevisionFromStore(ctx, store, txn)
	if err != nil && !storage.IsNotFound(err) {
		return "", nil, err
	}

	// read all bundle revisions from storage (if any exist)
	names, err := bundle.ReadBundleNamesFromStore(ctx, store, txn)
	if err != nil && !storage.IsNotFound(err) {
		return "", nil, err
	}

	br := make(map[string]server.BundleInfo, len(names))
	for _, name := range names {
		r, err := bundle.ReadBundleRevisionFromStore(ctx, store, txn, name)
		if err != nil && !storage.IsNotFound(err) {
			return "", nil, err
		}
		br[name] = server.BundleInfo{Revision: r}
	}

	return legacyRevision, br, nil
}

func (h *hndl) generateDecisionID() string {
	// Use the factory function if provided.
	// This is used mostly for testing.
	if h.decisionIDFactory != nil {
		return h.decisionIDFactory()
	}
	return ""
}

func defaultDecisionIDFactory() string {
	// Default to generating a new UUID for the decision ID.
	r, err := uuid.NewRandom()
	if err != nil {
		return ""
	}
	return r.String()
}
