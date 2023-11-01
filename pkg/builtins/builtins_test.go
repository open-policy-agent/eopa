package builtins_test

import (
	"os"
	"testing"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func TestMain(m *testing.M) {
	rego_vm.SetDefault(true)
	builtins.Init()

	os.Exit(m.Run())
}
