//go:build use_opa_fork

package rego_vm

import (
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/topdown"
)

// If the OPA Fork is available, use the external cancellation system.
func getExternalCancel(ectx *rego.EvalContext) topdown.Cancel {
	return ectx.ExternalCancel()
}
