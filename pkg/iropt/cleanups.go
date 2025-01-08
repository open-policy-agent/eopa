package iropt

import (
	"github.com/open-policy-agent/opa/v1/ir"
)

// Cleanup pass types.
// These optimization passes "clean up" after major passes have run, such as the LICM pass.
// Note: These should *never* be required for the correctness of a major pass type.

func EmptyLoopReplacementPass(policy *ir.Policy) *ir.Policy {
	plans := make([]*ir.Plan, 0, len(policy.Plans.Plans))
	for _, p := range policy.Plans.Plans {
		updatedBlocks, modified := BlockTransformPassBlocks(p.Blocks, EmptyLoopReplacement)
		if modified {
			plans = append(plans, &ir.Plan{Name: p.Name, Blocks: updatedBlocks})
			continue
		}
		plans = append(plans, p)
	}

	funcs := make([]*ir.Func, 0, len(policy.Funcs.Funcs))
	for _, f := range policy.Funcs.Funcs {
		updatedBlocks, modified := BlockTransformPassBlocks(f.Blocks, EmptyLoopReplacement)
		if modified {
			updatedFunc := *f
			updatedFunc.Blocks = updatedBlocks
			funcs = append(funcs, &updatedFunc)
			continue
		}
		funcs = append(funcs, f)
	}

	updatedPolicy := ir.Policy{
		Static: policy.Static,
		Plans:  &ir.Plans{Plans: plans},
		Funcs:  &ir.Funcs{Funcs: funcs},
	}
	return &updatedPolicy
}

// Replaces empty ScanStmt loops with a short sequence of non-looping
// instructions with the same early-exit effects as the loop.
func EmptyLoopReplacement(block *ir.Block) (*ir.Block, bool) {
	out := make([]ir.Stmt, 0, len(block.Stmts))
	modified := false
	for _, stmt := range block.Stmts {
		if x, ok := stmt.(*ir.ScanStmt); ok {
			// Empty loop? Replace it!
			if len(x.Block.Stmts) == 0 {
				out = append(out,
					// Check that the collection is defined.
					&ir.IsDefinedStmt{Source: ir.Local(x.Source), Location: x.Location},
					// Check that len(collection) > 0.
					&ir.MakeNumberIntStmt{Value: int64(0), Target: x.Key, Location: x.Location},
					&ir.LenStmt{Source: ir.Operand{Value: ir.Local(x.Source)}, Target: x.Value, Location: x.Location},
					&ir.NotEqualStmt{A: ir.Operand{Value: ir.Local(x.Key)}, B: ir.Operand{Value: ir.Local(x.Value)}, Location: x.Location},
				)
				modified = true
				continue
			}
		}
		out = append(out, stmt)
	}
	if modified {
		return &ir.Block{Stmts: out}, true
	}
	return block, false
}
