//go:build use_opa_fork

package builtins

import (
	"github.com/open-policy-agent/opa/ast"
)

func updateCaps() {
	ast.UpdateCapabilities = enterpriseOPAExtensions
}
