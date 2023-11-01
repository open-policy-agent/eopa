package iropt

import (
	"github.com/open-policy-agent/opa/ir"
)

var RegoVMIROptimizationPassSchedule []*IROptPass

// Generates a new optimization pass schedule for -O=0, taking into account
// -of and -ofno flags.
// Note(philip): Eventually, we'll add back the `cliEnableFlags` parameter,
// but for now, no passes need manual enabling at -O=0.
func NewIROptLevel0Schedule(cliEnableFlags, _ *OptimizationPassFlags) []*IROptPass {
	out := make([]*IROptPass, 0, 1)
	// HACK(philip): Temporarily disable LICM until we diagnose the cause
	// of incorrect results from the entitlements bundle test.
	if cliEnableFlags.LoopInvariantCodeMotion {
		p := &IROptPass{
			name:       "Loop Invariant Code Motion",
			metricName: "eopa-iropt-pass-licm",
			f:          LoopInvariantCodeMotionPass,
		}
		out = append(out, p)
	}
	return out
}

func NewIROptLevel1Schedule(cliEnableFlags, cliDisableFlags *OptimizationPassFlags) []*IROptPass {
	return NewIROptLevel0Schedule(cliEnableFlags, cliDisableFlags)
}

func NewIROptLevel2Schedule(cliEnableFlags, cliDisableFlags *OptimizationPassFlags) []*IROptPass {
	return NewIROptLevel0Schedule(cliEnableFlags, cliDisableFlags)
}

// Borrowed from OPA's compiler stage struct:
type IROptPass struct {
	name       string
	metricName string
	f          func(*ir.Policy) *ir.Policy
}

func RunPasses(policy *ir.Policy, schedule []*IROptPass) (*ir.Policy, error) {
	out := policy
	for _, pass := range schedule {
		out = pass.f(out)
	}
	return out, nil
}
