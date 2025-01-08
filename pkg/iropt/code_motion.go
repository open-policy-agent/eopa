package iropt

import "github.com/open-policy-agent/opa/v1/ir"

// ---------------------------------------------------------------------------
// Code Motion helpers

// Returns (possibly rewritten) stmt, and whether it was modified or not.
func RewriteBreakStmtsForLifting(stmt ir.Stmt, depth uint32) (ir.Stmt, bool) {
	switch x := stmt.(type) {
	case *ir.BreakStmt:
		// Nested break, or direct target-to-lift?
		if depth > 0 {
			// This is a nested BreakStmt that has a jump target above the
			// topmost lifted stmt's scope. Rewrite with Index--.
			if x.Index >= depth {
				updatedBreakStmt := *x
				updatedBreakStmt.Index--
				return &updatedBreakStmt, true
			}
			// Else leave as-is.
			return x, false
		}
		// Otherwise, this BreakStmt is the target-to-lift.
		// Delete Index == 0 break stmts upon lifting.
		if x.Index == 0 {
			return nil, true
		}
		// Else, rewrite with Index--.
		updatedBreakStmt := *x
		updatedBreakStmt.Index--
		return &updatedBreakStmt, true
	case *ir.BlockStmt:
		modified := false
		blocks := make([]*ir.Block, 0, len(x.Blocks))
		for _, b := range x.Blocks {
			block := make([]ir.Stmt, 0, len(b.Stmts))
			for _, s := range b.Stmts {
				updatedStmt, modifiedStmt := RewriteBreakStmtsForLifting(s, depth+1)
				block = append(block, updatedStmt)
				modified = modified || modifiedStmt
			}
			blocks = append(blocks, &ir.Block{Stmts: block})
		}
		if !modified {
			return x, false
		}
		updatedBlockStmt := *x
		updatedBlockStmt.Blocks = blocks
		return &updatedBlockStmt, true
	case *ir.NotStmt:
		modified := false
		block := make([]ir.Stmt, 0, len(x.Block.Stmts))
		for _, s := range x.Block.Stmts {
			updatedStmt, modifiedStmt := RewriteBreakStmtsForLifting(s, depth+1)
			block = append(block, updatedStmt)
			modified = modified || modifiedStmt
		}
		if !modified {
			return x, false
		}
		updatedNotStmt := *x
		updatedNotStmt.Block = &ir.Block{Stmts: block}
		return &updatedNotStmt, true
	case *ir.WithStmt:
		modified := false
		block := make([]ir.Stmt, 0, len(x.Block.Stmts))
		for _, s := range x.Block.Stmts {
			updatedStmt, modifiedStmt := RewriteBreakStmtsForLifting(s, depth+1)
			block = append(block, updatedStmt)
			modified = modified || modifiedStmt
		}
		if !modified {
			return x, false
		}
		updatedWithStmt := *x
		updatedWithStmt.Block = &ir.Block{Stmts: block}
		return &updatedWithStmt, true
	case *ir.ScanStmt:
		modified := false
		block := make([]ir.Stmt, 0, len(x.Block.Stmts))
		for _, s := range x.Block.Stmts {
			updatedStmt, modifiedStmt := RewriteBreakStmtsForLifting(s, depth+1)
			block = append(block, updatedStmt)
			modified = modified || modifiedStmt
		}
		if !modified {
			return x, false
		}
		updatedScanStmt := *x
		updatedScanStmt.Block = &ir.Block{Stmts: block}
		return &updatedScanStmt, true
	default:
		return x, false
	}
}
