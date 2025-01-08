//go:build !use_opa_fork

package rego_vm

import (
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown"
)

// If not building with the OPA Fork, just use our own canceller.
func getExternalCancel(*rego.EvalContext) topdown.Cancel {
	return nil
}
