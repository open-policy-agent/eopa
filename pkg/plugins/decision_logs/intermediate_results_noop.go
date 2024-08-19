//go:build !use_opa_fork

package decisionlogs

import "github.com/open-policy-agent/opa/plugins/logs"

func intermediateResults(e logs.EventV1) (map[string]any, bool) {
	return nil, false
}
