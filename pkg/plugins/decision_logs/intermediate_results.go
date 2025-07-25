package decisionlogs

import "github.com/open-policy-agent/opa/v1/plugins/logs"

func intermediateResults(e logs.EventV1) (map[string]any, bool) {
	return e.IntermediateResults, e.IntermediateResults != nil
}
