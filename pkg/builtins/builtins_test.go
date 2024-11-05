package builtins_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/types"
	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/library"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func TestMain(m *testing.M) {
	rego_vm.SetDefault(true)
	builtins.Init()
	if err := library.Init(); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func TestAllBuiltinsHaveMetadata(t *testing.T) {
	docsExceptions := []string{"rego.eval", "ucast.as_sql"} // these don't have public docs (yet)
	for _, b := range builtins.Builtins {
		t.Run(b.Name, func(t *testing.T) {
			namedAndDescribed(t, "arg", b.Decl.NamedFuncArgs().Args...)
			namedAndDescribed(t, "res", b.Decl.NamedResult())
			found := false
			for _, c := range b.Categories {
				if strings.HasPrefix(c, "url=") {
					found = true
				}
			}
			if !found && !slices.Contains(docsExceptions, b.Name) {
				t.Error("missing 'url=' category")
			}
		})
	}
}

func namedAndDescribed(t *testing.T, typ string, args ...types.Type) {
	t.Helper()
	for i, arg := range args {
		t.Run(fmt.Sprintf("%s=%d", typ, i), func(t *testing.T) {
			typ, ok := arg.(*types.NamedType)
			if !ok {
				t.Fatalf("expected arg to be %T", typ)
			}
			if typ.Name == "" {
				t.Error("empty name")
			}
			if typ.Descr == "" {
				t.Error("empty description")
			}
		})
	}
}
