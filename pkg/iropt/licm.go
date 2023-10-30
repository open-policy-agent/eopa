package iropt

import (
	"github.com/open-policy-agent/opa/ir"
)

// Encodes the state of preamble, postamble, and loop body instructions for the loop.
type LoopTransformState struct {
	Before []ir.Stmt
	After  []ir.Stmt
	Loop   ir.Stmt // ScanStmt with a body appended.
}

// The pass design allows us to wire this into the global pass structure.
func LoopInvariantCodeMotionPass(policy *ir.Policy) *ir.Policy {
	plans := make([]*ir.Plan, 0, len(policy.Plans.Plans))
	for _, p := range policy.Plans.Plans {
		updatedBlocks, modified := LoopPassBlocks(p.Blocks, LoopInvariantCodeMotion)
		if modified {
			plans = append(plans, &ir.Plan{Name: p.Name, Blocks: updatedBlocks})
			continue
		}
		plans = append(plans, p)
	}

	funcs := make([]*ir.Func, 0, len(policy.Funcs.Funcs))
	for _, f := range policy.Funcs.Funcs {
		updatedBlocks, modified := LoopPassBlocks(f.Blocks, LoopInvariantCodeMotion)
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

// Recursively descends the block list, until it finds the innermost loop
// body, and then applies the loop transform repeatedly until no further
// modifications are made.
func LoopPassBlocks(blocks []*ir.Block, f func(LoopTransformState) (LoopTransformState, bool)) ([]*ir.Block, bool) {
	out := make([]*ir.Block, 0, len(blocks))
	modifiedBL := false
	// For each block...
	for _, block := range blocks {
		resultBlock, modified := LoopPassBlock(block, f)
		// Was the block modified?
		if modified {
			out = append(out, resultBlock)
			modifiedBL = true
			continue
		}
		// Otherwise, append original block.
		out = append(out, block)
	}
	return out, modifiedBL
}

func LoopPassBlock(block *ir.Block, f func(LoopTransformState) (LoopTransformState, bool)) (*ir.Block, bool) {
	out := ir.Block{Stmts: make([]ir.Stmt, 0, len(block.Stmts))}
	modifiedBlock := false
	// For each statement in the block...
	for _, stmt := range block.Stmts {
		// Recurse down into nested-block types, including ScanStmt's.
		// In the nested ScanStmt case, this transforms the inner ScanStmt's first, and works outwards/upwards in the nesting stack.
		// We don't worry about block lists, because they never occur in a spot where we'd need to propagate information upwards and through them.
		// Lifting nested Scans should occur naturally by lifting the parent Stmt.
		switch x := stmt.(type) {
		case *ir.BlockStmt:
			resultBL, modified := LoopPassBlocks(x.Blocks, f)
			if modified {
				updatedBlock := ir.BlockStmt{
					Blocks:   resultBL,
					Location: x.Location,
				}
				out.Stmts = append(out.Stmts, &updatedBlock)
				modifiedBlock = true
				continue
			}
			out.Stmts = append(out.Stmts, x)
		case *ir.NotStmt:
			resultBlock, modified := LoopPassBlock(x.Block, f)
			if modified {
				updatedNot := ir.NotStmt{
					Block:    resultBlock,
					Location: x.Location,
				}
				out.Stmts = append(out.Stmts, &updatedNot)
				modifiedBlock = true
				continue
			}
			out.Stmts = append(out.Stmts, x)
		case *ir.WithStmt:
			resultBlock, modified := LoopPassBlock(x.Block, f)
			if modified {
				updatedWith := ir.WithStmt{
					Local:    x.Local,
					Path:     x.Path,
					Value:    x.Value,
					Block:    resultBlock,
					Location: x.Location,
				}
				out.Stmts = append(out.Stmts, &updatedWith)
				modifiedBlock = true
				continue
			}
			out.Stmts = append(out.Stmts, x)
		case *ir.ScanStmt:
			// Recurse, then modify loop at this level.
			resultBlock, modified := LoopPassBlock(x.Block, f)
			updatedScan := *x
			// Run the function on the loop until it reaches a fixpoint.
			// Then patch up the block as needed.
			if modified {
				updatedScan.Block = resultBlock
			}
			// modifiedLoop := true
			lts := LoopTransformState{Loop: &updatedScan}
			ltsLICM, _ := f(lts)
			// Append lifted instructions, the loop, and any post-loop sequence.
			out.Stmts = append(out.Stmts, ltsLICM.Before...)
			out.Stmts = append(out.Stmts, ltsLICM.Loop)
			out.Stmts = append(out.Stmts, ltsLICM.After...)
			modifiedBlock = true
		default:
			out.Stmts = append(out.Stmts, stmt)
		}
	}
	return &out, modifiedBlock
}

// Performs Loop Invariant Code Motion for this loop.
// Behavior Notes:
//   - Assumes it's the innermost loop, and that LICM has already happened for any nested ScanStmt loops in its loop body.
//   - Runs until it hits a fixpoint, which  happens when there are no further instructions to move.
//
// Edge-Case Notes:
//   - BreakStmt :: Has an implicit control dependency on all upstream instructions in the block.
//     -- Resolved by *always* being the last thing moved out of a loop. If it is not the only member of the loop, it cannot be moved.
//     -- If lifted, it usually has to be rewritten, to ensure its jump target is the same.
//     --- We don't have to recursively rewrite the levels, because the jumps are *relative*, and only direct movement of the statement requires a rewrite, because that's changing its relative placement in the block hierarchy, relative to sibling blocks.
//   - AssignVarOnceStmt, ObjectInsertOnceStmt :: Implicit control dependency, similar to BreakStmt.
//     -- We always lift these last out of a loop. No rewriting is performed.
//     --- Note(philip): This may prove unsafe, in which case we'll just have to treat them as unmovable.
//   - Nondeterministic Builtin function calls :: Generally cannot be moved out of the loop. Have ordering dependencies relative to each other. (Ex: http.send calls)
//     -- Applies to: some CallStmt, almost all CallDynamicStmt
//     -- HACK: Resolved by never lifting ND builtins out of the loop, and thus they'll end up being in-order at the end.
//     -- TODO: We *can* do better down the road, but for now, it's easier to just treat them as unmovable.
//     --- The down-the-road way of "doing better" will be to track which ND builtin calls are really unmovable, and treating them as movement barriers.
//
// Mark-and-Lift Algorithm:
//   - Because sometimes it's convenient to create a temp var in a loop, we have to check the reaching definition property of each instruction in the loop-- does any downstream instruction overwrite that instruction's write target register(s)?
//   - We can continue to do a linear forward pass by marking each liftable instruction index, and then only at the end (before the next pass) lifting all the truly safe-to-lift instructions at once.
func LoopInvariantCodeMotion(state LoopTransformState) (LoopTransformState, bool) {
	out := state // Copy incoming state for later.
	origScan := state.Loop.(*ir.ScanStmt)
	// Deep copy the stmt list.
	loopStmts := make([]ir.Stmt, len(origScan.Block.Stmts))
	copy(loopStmts, origScan.Block.Stmts)

	// We retrieve the Unsat info from the loop body, and if it's all the
	// register args for an instruction, we can hoist it.
	liftedStmtsPre := make([]ir.Stmt, 0)
	liftedStmtsPost := make([]ir.Stmt, 0)
	modified := true
	for modified {
		satByLoop := make(SatMap)
		satByLoop[origScan.Source] = -1
		satByLoop[origScan.Key] = -1
		satByLoop[origScan.Value] = -1
		modified = false

		nextLoopStmts := []ir.Stmt{}
		stmtIsLiftableToPreamble := make([]bool, len(loopStmts))
		// The marking step of "mark-and-lift":
		for idx, stmt := range loopStmts {
			outRegs, inRegs := extractLocalRefs(stmt)
			// Determine if we have an intra-loop data dependency:
			hasLoopDep := false
			for _, local := range inRegs {
				// Depends on something inside the loop.
				if _, ok := satByLoop[local]; ok {
					hasLoopDep = true
					break
				}
			}
			// Mark out register(s) for downstream consumers.
			for _, local := range outRegs {
				// Mark upstream statement as unsafe to lift if this instr writes it.
				if prevIdx, ok := satByLoop[local]; ok && prevIdx != -1 {
					stmtIsLiftableToPreamble[prevIdx] = false
				}
				satByLoop[local] = idx
			}
			if hasLoopDep {
				stmtIsLiftableToPreamble[idx] = false
				continue // Skip lifting this instruction; we can't move it right now.
			}

			switch stmt.(type) {
			// Edge case: BreakStmt.
			// It is only safe to lift (with rewriting!) if no instructions precede it.
			case *ir.BreakStmt:
				if len(loopStmts) == 1 {
					stmtIsLiftableToPreamble[idx] = true
					continue
				}
				stmtIsLiftableToPreamble[idx] = false
			// Edge cases: AssignVarOnceStmt, ObjectInsertOnceStmt
			// Note(philip) It may be safe to lift this if no instructions precede it.
			case *ir.AssignVarOnceStmt:
				stmtIsLiftableToPreamble[idx] = false
			case *ir.ObjectInsertOnceStmt:
				stmtIsLiftableToPreamble[idx] = false
			// Edge case: Non-deterministic builtin calls.
			// These cannot be safely lifted, so we leave them in place.
			case *ir.CallDynamicStmt:
				stmtIsLiftableToPreamble[idx] = false
			case *ir.CallStmt:
				// TODO(philip): Check x.Func against NDB list.
				stmtIsLiftableToPreamble[idx] = false
			default:
				// Otherwise, we can lift this statement.
				stmtIsLiftableToPreamble[idx] = true
			}
		}
		// The lifting step of "mark-and-lift":
		for idx, stmt := range loopStmts {
			// Statement survived the marking phase as being safe. Lift it!
			if stmtIsLiftableToPreamble[idx] {
				rewrittenStmt, _ := RewriteBreakStmtsForLifting(stmt, 0)
				// Note(philip): We need this nil check because
				// RewriteBreakStmtsForLifting will mark liftable Index==0
				// BreakStmts for deletion by returning nil.
				if rewrittenStmt != nil {
					liftedStmtsPre = append(liftedStmtsPre, rewrittenStmt)
				}
				modified = true
			} else {
				nextLoopStmts = append(nextLoopStmts, stmt)
			}
		}
		// Prime the next pass with the non-lifted instructions.
		loopStmts = nextLoopStmts
		if !modified {
			break
		}
	}

	// Tack lifted statements onto the preamble.
	out.Before = append(out.Before, liftedStmtsPre...)
	out.After = append(out.After, liftedStmtsPost...)
	out.Loop = &ir.ScanStmt{
		Source:   origScan.Source,
		Key:      origScan.Key,
		Value:    origScan.Value,
		Block:    &ir.Block{Stmts: loopStmts},
		Location: origScan.Location,
	}

	return out, len(liftedStmtsPre) > 0 || len(liftedStmtsPost) > 0
}
