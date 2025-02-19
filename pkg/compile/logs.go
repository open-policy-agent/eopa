package compile

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/server"
	"github.com/open-policy-agent/opa/v1/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle"
)

// decisionLogType is injected under custom.type in the DL entry
const decisionLogType = "eopa.styra.com/compile"

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

func dlog(ctx context.Context,
	path string,
	result *any,
	orig *CompileRequestV1,
	request *compileRequest,
	unknowns []string,
	m metrics.Metrics,
	store storage.Store,
	txn storage.Transaction,
) (*server.Info, error) {
	// this section mirrors opa/v1/server/server.go: (decisionLogger).Log(...)
	var rctx logging.RequestContext
	if r, ok := logging.FromContext(ctx); ok {
		rctx = *r
	}

	var httpRctx logging.HTTPRequestContext
	httpRctxVal, _ := logging.HTTPRequestContextFromContext(ctx)
	if httpRctxVal != nil {
		httpRctx = *httpRctxVal
	}
	legacyRevision, bundles, err := getRevisions(ctx, store, txn)
	if err != nil {
		return nil, err
	}

	info := &server.Info{
		// generic fields
		Timestamp:          time.Now().UTC(),
		DecisionID:         generateDecisionID(),
		RemoteAddr:         rctx.ClientAddr,
		HTTPRequestContext: httpRctx,
		Input:              orig.Input,
		InputAST:           request.Input,
		Error:              nil,
		Metrics:            m,
		RequestID:          rctx.ReqID,
		Txn:                txn,
		Bundles:            bundles,
		Revision:           legacyRevision, // deprecated

		// Compile API specific fields
		Results: result,
		Path:    path,
		Query:   orig.Query,
	}
	addCustom(map[string]any{
		"options":  orig.Options,
		"unknowns": unknowns,
		"type":     decisionLogType,
	}, info)
	return info, nil
}

func generateDecisionID() string {
	r, err := uuid.NewRandom()
	if err != nil {
		return ""
	}
	return r.String()
}
