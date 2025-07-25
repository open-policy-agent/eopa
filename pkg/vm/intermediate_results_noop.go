package vm

import "context"

func getIntermediateResults(context.Context) map[string]any {
	// SDK doesn't use the forked OPA, so no map to return.
	return nil
}
