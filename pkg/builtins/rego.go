package builtins

import (
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/types"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins/rego"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

var (
	regoEval = &ast.Builtin{
		Name: rego.RegoEvalName,
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("rego eval request"),
			),
			types.Named("output", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("rego eval result"),
		),
		Nondeterministic: true,
	}

	regoCompile = &ast.Builtin{
		Name: vm.RegoCompileName,
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("rego compile request"),
			),
			types.Named("output", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("rego compile result"),
		),
		Nondeterministic: true,
	}
)

func init() {
	RegisterBuiltinFunc(rego.RegoEvalName, vm.BuiltinRegoEval)
	RegisterBuiltinFunc(vm.RegoCompileName, nil)
}
