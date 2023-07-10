//go:build use_opa_fork

package rego_vm

import (
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/tracing"
)

func tracingOpts(e *rego.EvalContext) tracing.Options {
	return e.TracingOpts()
}
