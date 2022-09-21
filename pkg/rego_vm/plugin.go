// Package rego_vm contains the rego target plugin to be used with OPA.
package rego_vm

import (
	"context"

	bjson "github.com/StyraInc/load/pkg/json"
	regovm "github.com/StyraInc/load/pkg/rego_vm/vm"
	inmem "github.com/StyraInc/load/pkg/store"
	"github.com/StyraInc/load/pkg/vm"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
)

const Name = "rego_target_vm"

func init() {
	rego.RegisterPlugin(Name, &vmp{})
}

type vmp struct{}

// We want this target to be the default.
func (*vmp) IsTarget(t string) bool {
	return t == "" || t == "vm"
}

// TODO(sr): move store and tx into PrepareOption?
func (*vmp) PrepareForEval(ctx context.Context, policy *ir.Policy, store storage.Store, txn storage.Transaction, _ ...rego.PrepareOption) (rego.TargetPluginEval, error) {
	var data bjson.Json
	var err error

	switch s := store.(type) {
	case inmem.BJSONReader:
		data, err = s.ReadBJSON(ctx, txn, storage.Path{})
		if err != nil {
			return nil, err
		}
	default:
		blob, err := s.Read(ctx, txn, storage.Path{})
		if err != nil {
			return nil, err
		}
		data = bjson.MustNew(blob)
	}

	ops := regovm.NewDataOperations()
	executable, err := vm.NewCompiler(ops).WithPolicy(policy).Compile()
	if err != nil {
		return nil, err
	}

	return &vme{
		vm: vm.NewVM().WithDataOperations(ops).WithDataJSON(data).WithExecutable(executable),
	}, nil
}

type vme struct {
	vm *vm.VM
}

func (t *vme) Eval(ctx context.Context, ectx *rego.EvalContext) (ast.Value, error) {
	input := ectx.RawInput()
	if p := ectx.ParsedInput(); p != nil {
		i := interface{}(p)
		input = &i
	}

	var s *vm.Statistics
	s, ctx = vm.WithStatistics(ctx)
	result, err := t.vm.Eval(ctx, "eval", vm.EvalOpts{
		Metrics:                ectx.Metrics(),
		Input:                  input,
		Time:                   ectx.Time(),
		Seed:                   ectx.Seed(),
		InterQueryBuiltinCache: ectx.InterQueryBuiltinCache(),
		PrintHook:              ectx.PrintHook(),
		StrictBuiltinErrors:    ectx.StrictBuiltinErrors(),
	})
	if err != nil {
		if err == vm.ErrVarAssignConflict {
			return nil, &topdown.Error{
				Code:    topdown.ConflictErr,
				Message: "complete rules must not produce multiple outputs",
			}
		}

		return nil, err
	}
	statsToMetrics(ectx.Metrics(), s)
	return result.(ast.Value), nil
}
