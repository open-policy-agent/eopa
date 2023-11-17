package iropt

import (
	"github.com/open-policy-agent/opa/ir"
)

// Block-scoped transformation pass utilities.
// These functions can be used to implement "peephole"-style optimization
// passes, as well as more general transforms. They are most appropriate
// when only block-local context matters, and Func/Plan-level information
// isn't strictly needed.

// Recursively descends the block list, applying the block transformation
// to the deepest-nested blocks first.
func BlockTransformPassBlocks(blocks []*ir.Block, f func(*ir.Block) (*ir.Block, bool)) ([]*ir.Block, bool) {
	out := make([]*ir.Block, 0, len(blocks))
	modifiedBL := false
	// For each block...
	for _, block := range blocks {
		resultBlock, modified := BlockTransformPassBlock(block, f)
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

// Recursively applies the transform for lower blocks, and after assembling
// the (possibly transformed) instructions into a block, applies the
// transform at this level of the plan before returning the results.
func BlockTransformPassBlock(block *ir.Block, f func(*ir.Block) (*ir.Block, bool)) (*ir.Block, bool) {
	out := ir.Block{Stmts: make([]ir.Stmt, 0, len(block.Stmts))}
	modifiedBlock := false
	// For each statement in the block...
	for _, stmt := range block.Stmts {
		// Recurse down into nested-block types.
		switch x := stmt.(type) {
		case *ir.BlockStmt:
			resultBL, modified := BlockTransformPassBlocks(x.Blocks, f)
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
			resultBlock, modified := BlockTransformPassBlock(x.Block, f)
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
			resultBlock, modified := BlockTransformPassBlock(x.Block, f)
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
			resultBlock, modified := BlockTransformPassBlock(x.Block, f)
			if modified {
				updatedScan := ir.ScanStmt{
					Source:   x.Source,
					Key:      x.Key,
					Value:    x.Value,
					Block:    resultBlock,
					Location: x.Location,
				}
				out.Stmts = append(out.Stmts, &updatedScan)
				modifiedBlock = true
				continue
			}
			out.Stmts = append(out.Stmts, x)
		default:
			out.Stmts = append(out.Stmts, stmt)
		}
	}
	result, modified := f(&out)
	if modified {
		return result, true
	}
	return &out, modifiedBlock
}
