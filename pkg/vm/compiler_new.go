package vm

import (
	"context"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
)

func getCompiler(ctx context.Context, m metrics.Metrics, modules map[string]any) (*ast.Compiler, error) {
	comp := ast.NewCompiler().
		// WithUnsafeBuiltins(r.unsafeBuiltins).
		// WithBuiltins(r.builtinDecls).
		// WithCapabilities(r.capabilities). // TODO
		WithEnablePrintStatements(true).
		WithUseTypeCheckAnnotations(true).
		WithEvalMode(ast.EvalModeTopdown).
		WithDefaultRegoVersion(ast.DefaultRegoVersion).
		WithMetrics(m)

	mods := make(map[string]*ast.Module, len(modules))
	for k := range modules {
		var err error
		mods[k], err = ast.ParseModuleWithOpts(k, modules[k].(string),
			ast.ParserOptions{ProcessAnnotation: true, RegoVersion: ast.RegoV1})
		if err != nil {
			return nil, err
		}
	}

	comp.Compile(mods)
	if len(comp.Errors) > 0 {
		return nil, comp.Errors
	}
	return comp, nil
}
