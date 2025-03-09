//go:build !use_opa_fork

package vm

import "errors"

func regoCompileBuiltin(_, _ *State, _ []Value) error {
	return errors.New("rego.compile is not supported in source-availability mode")
}
