package builtins

import (
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/types"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins/rego"
)

var (
	regoEval = &ast.Builtin{
		Name: rego.RegoEvalName,
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))),
			),
			types.Named("output", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))),
		),
		Nondeterministic: true,
	}
)
