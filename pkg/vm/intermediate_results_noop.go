//go:build !use_opa_fork

package vm

import "context"

func getIntermediateResults(ctx context.Context) map[string]interface{} {
	// SDK doesn't use the forked OPA, so no map to return.
	return nil
}
