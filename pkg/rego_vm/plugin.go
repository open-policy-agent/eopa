// Package rego_vm contains the rego target plugin to be used with OPA.
package rego_vm

import (
	"context"
	"crypto/rand"
	"sync"

	dl "github.com/styrainc/load-private/pkg/plugins/decision_logs"
	"github.com/styrainc/load-private/pkg/plugins/impact"
	"github.com/styrainc/load-private/pkg/vm"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

const Name = "rego_target_vm"

func init() {
	rego.RegisterPlugin(Name, &vmp{})
}

var limits *vm.Limits
var limitsMtx sync.Mutex

// SetLimits allows changing the *vm.Limits passed to the VM with EvalOpts.
func SetLimits(instr int64) {
	limitsMtx.Lock()
	limits = &vm.Limits{Instructions: instr}
	limitsMtx.Unlock()
}

type vmp struct{}

// We want this target to be the default.
func (*vmp) IsTarget(t string) bool {
	return t == "" || t == "vm"
}

// TODO(sr): move store and tx into PrepareOption?
func (*vmp) PrepareForEval(_ context.Context, policy *ir.Policy, _ ...rego.PrepareOption) (rego.TargetPluginEval, error) {
	executable, err := vm.NewCompiler().WithPolicy(policy).Compile()
	if err != nil {
		return nil, err
	}

	return &vme{
		vm: vm.NewVM().WithExecutable(executable),
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

	v := t.vm.WithDataNamespace(ectx.Transaction())

	result, err := v.Eval(ctx, "eval", vm.EvalOpts{
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
		Capabilities:           ectx.Capabilities(),
		Limits:                 limits,
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

	go impact.Enqueue(ctx, ectx, result)
	if err := dl.Log(ctx, ectx, result, err); err != nil {
		return nil, err
	}
	return result, nil
}

func runtime(rt ast.Value) ast.Value {
	if rt == nil {
		return ast.NewObject()
	}
	return rt
}
