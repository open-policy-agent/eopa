package vm

import (
	"context"

	"github.com/open-policy-agent/opa/v1/server"
)

// TODO: Move more of the intermediate results machinery here.

func getIntermediateResults(ctx context.Context) map[string]any {
	// Result is recorded into context in statements call.Execute.
	v := ctx.Value(server.IntermediateResultsContextKey{})
	if v == nil {
		return nil
	}

	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}

	return m
}
