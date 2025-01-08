//go:build !use_opa_fork

package rego_vm

import (
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/tracing"
)

func tracingOpts(*rego.EvalContext) tracing.Options {
	return nil
}
