package iropt

import "github.com/open-policy-agent/opa/ir"

// Encodes the state of preamble, postamble, and loop body instructions for the loop.
type LoopTransformState struct {
	Before []ir.Stmt
	After  []ir.Stmt
	Loop   ir.Stmt // ScanStmt with a body appended.
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
