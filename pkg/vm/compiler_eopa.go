//go:build use_opa_fork

package vm

import (
	"context"
	"errors"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
)

func getCompiler(ctx context.Context, m metrics.Metrics, _ map[string]any) (*ast.Compiler, error) {
	c, ok := ast.CompilerFromContext(ctx)
	if !ok {
		return nil, errors.New("expected compiler")
	}

	comp := ast.NewCompiler().
		WithEnablePrintStatements(true).
		WithUseTypeCheckAnnotations(true).
		WithEvalMode(ast.EvalModeTopdown).
		WithDefaultRegoVersion(ast.DefaultRegoVersion).
		WithMetrics(m)

	// NB(sr): This looks funny, but the first thing Compile()
	// does is copy the modules. No need to copy them ourselves
	// before passing them.
	comp.Compile(c.Modules)

	if len(comp.Errors) > 0 {
		return nil, comp.Errors
	}
	return comp, nil
}
