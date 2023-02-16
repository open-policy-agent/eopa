// Package rego_vm contains the rego target plugin to be used with OPA.
package rego_vm

import (
	"context"
	"crypto/rand"

	bjson "github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/plugins/impact"
	regovm "github.com/styrainc/load-private/pkg/rego_vm/vm"
	inmem "github.com/styrainc/load-private/pkg/store"
	"github.com/styrainc/load-private/pkg/vm"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
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

func (t *vme) Eval(ctx context.Context, ectx *rego.EvalContext, rt ast.Value) (ast.Value, error) {
	input := ectx.RawInput()
	if p := ectx.ParsedInput(); p != nil {
		i := interface{}(p)
		input = &i
	}

	ectx.Metrics().Timer(evalTimer).Start()
	var s *vm.Statistics
	s, ctx = vm.WithStatistics(ctx)

	seed := ectx.Seed()
	if seed == nil {
		seed = rand.Reader
	}
	result, err := t.vm.Eval(ctx, "eval", vm.EvalOpts{
		Metrics:                ectx.Metrics(),
		Input:                  input,
		Time:                   ectx.Time(),
		Seed:                   seed,
		Runtime:                runtime(rt),
		Cache:                  builtins.Cache{},
		NDBCache:               ectx.NDBCache(),
		InterQueryBuiltinCache: ectx.InterQueryBuiltinCache(),
		PrintHook:              ectx.PrintHook(),
		StrictBuiltinErrors:    ectx.StrictBuiltinErrors(),
	})
	ectx.Metrics().Timer(evalTimer).Stop()
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

	go impact.Enqueue(ctx, ectx, result.(ast.Value))
	return result.(ast.Value), nil
}

func runtime(rt ast.Value) ast.Value {
	if rt == nil {
		return ast.NewObject()
	}
	return rt
}
