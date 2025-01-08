//go:build use_opa_fork

package builtins

import (
	"github.com/open-policy-agent/opa/v1/ast"
)

func updateCaps() {
	ast.UpdateCapabilities = enterpriseOPAExtensions
}
