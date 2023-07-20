package rego

import (
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
)

const (
	RegoEvalName = "rego.eval"
)

var (
	// BuiltinRegoEval is a hook for the implementation in the pkg/vm package. This is to avoid circular references.
	BuiltinRegoEval func(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error

	RegoEvalLatencyMetricKey    = "rego_builtin_rego_eval"
	RegoEvalInterQueryCacheHits = RegoEvalLatencyMetricKey + "_interquery_cache_hits"
	RegoEvalIntraQueryCacheHits = RegoEvalLatencyMetricKey + "_intraquery_cache_hits"
)

func RegisterBuiltinRegoEval(f func(topdown.BuiltinContext, []*ast.Term, func(*ast.Term) error) error) {
	topdown.RegisterBuiltinFunc(RegoEvalName, f)
}
