package builtins

import (
	"github.com/open-policy-agent/opa/ast"
)

// Builtins is the registry of built-in functions supported by Enterprise OPA.
// Call RegisterBuiltin to add a new built-in.
var Builtins []*ast.Builtin

// RegisterBuiltin adds a new built-in function to the registry.
func RegisterBuiltin(b *ast.Builtin) {
	Builtins = append(Builtins, b)
	BuiltinMap[b.Name] = b
	if len(b.Infix) > 0 {
		BuiltinMap[b.Infix] = b
	}
}

// BuiltinMap provides a convenient mapping of built-in names to
// built-in definitions.
var BuiltinMap map[string]*ast.Builtin

// DefaultBuiltins is the registry of built-in functions supported in Enterprise
// OPA by default. When adding a new built-in function to Enterprise OPA, update
// this list.
var DefaultBuiltins = [...]*ast.Builtin{
	// SQL/database builtins.
	sqlSend,
}

func init() {
	BuiltinMap = map[string]*ast.Builtin{}
	for _, b := range DefaultBuiltins {
		RegisterBuiltin(b)     // Only used for generating Enterprise OPA-specific capabilities.
		ast.RegisterBuiltin(b) // Normal builtin registration with OPA.
	}
}
