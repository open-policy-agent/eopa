//go:build use_opa_fork

package vm

import (
	"context"
	"errors"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/rego"
)

func getCompiler(ctx context.Context, _ metrics.Metrics, _ map[string]any) (*ast.Compiler, error) {
	if c, ok := ast.CompilerFromContext(ctx); ok {
		return c, nil
	}
	return nil, errors.New("expected compiler")
}

func extraRegoOpts() []func(*rego.Rego) {
	return []func(*rego.Rego){
		rego.EvalMode(ast.EvalModeTopdown),
	}
}
