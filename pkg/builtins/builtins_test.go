// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package builtins_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/open-policy-agent/opa/v1/types"

	"github.com/open-policy-agent/eopa/pkg/builtins"
	"github.com/open-policy-agent/eopa/pkg/library"
	"github.com/open-policy-agent/eopa/pkg/rego_vm"
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
	for _, b := range builtins.Builtins {
		t.Run(b.Name, func(t *testing.T) {
			namedAndDescribed(t, "arg", b.Decl.NamedFuncArgs().Args...)
			namedAndDescribed(t, "res", b.Decl.NamedResult())
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
