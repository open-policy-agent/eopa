package iropt

import "github.com/open-policy-agent/opa/v1/ir"

// Returns a Local, true if retrieval succeeded.
// Else, returns 0, false.
func localFromOperand(op ir.Operand) (ir.Local, bool) {
	if local, ok := op.Value.(ir.Local); ok {
		return local, true
	}
	if local, ok := op.Value.(*ir.Local); ok {
		return *local, true
	}
	return 0, false
}

// Returns outRegs, inRegs
// Recurses as necessary to extract deeply-nested dependencies.
func extractLocalRefs(stmt ir.Stmt) ([]ir.Local, []ir.Local) {
	switch x := stmt.(type) {
	case *ir.ReturnLocalStmt:
		return nil, []ir.Local{x.Source}
	case *ir.CallStmt:
		args := make([]ir.Local, 0, len(x.Args))
		for _, op := range x.Args {
			if local, ok := localFromOperand(op); ok {
				args = append(args, local)
			}
		}
		return []ir.Local{x.Result}, args
	case *ir.CallDynamicStmt:
		args := make([]ir.Local, 0, len(x.Args))
		args = append(args, x.Args...)
		path := make([]ir.Local, 0, len(x.Path))
		for _, op := range x.Path {
			if local, ok := localFromOperand(op); ok {
				path = append(path, local)
			}
		}
		return []ir.Local{x.Result}, append(args, path...)
	case *ir.BlockStmt:
		outRegs := []ir.Local{}
		inRegs := []ir.Local{}
		for _, b := range x.Blocks {
			for _, stmt := range b.Stmts {
				nestedOutRegs, nestedInRegs := extractLocalRefs(stmt)
				outRegs = append(outRegs, nestedOutRegs...)
				inRegs = append(inRegs, nestedInRegs...)
			}
		}
		return outRegs, inRegs
	case *ir.BreakStmt:
		return []ir.Local{}, []ir.Local{}
	case *ir.DotStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		if local, ok := localFromOperand(x.Key); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Target}, inRegs
	case *ir.LenStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Target}, inRegs
	case *ir.ScanStmt:
		outRegs := []ir.Local{x.Key, x.Value}
		inRegs := []ir.Local{x.Source}
		for _, stmt := range x.Block.Stmts {
			nestedOutRegs, nestedInRegs := extractLocalRefs(stmt)
			outRegs = append(outRegs, nestedOutRegs...)
			inRegs = append(inRegs, nestedInRegs...)
		}
		return outRegs, inRegs
	case *ir.NotStmt:
		outRegs := []ir.Local{}
		inRegs := []ir.Local{}
		for _, stmt := range x.Block.Stmts {
			nestedOutRegs, nestedInRegs := extractLocalRefs(stmt)
			outRegs = append(outRegs, nestedOutRegs...)
			inRegs = append(inRegs, nestedInRegs...)
		}
		return outRegs, inRegs
	case *ir.AssignIntStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.AssignVarStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Target}, inRegs
	case *ir.AssignVarOnceStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Target}, inRegs
	case *ir.ResetLocalStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeNullStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeNumberIntStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeNumberRefStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeArrayStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeObjectStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.MakeSetStmt:
		return []ir.Local{x.Target}, []ir.Local{}
	case *ir.EqualStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.A); ok {
			inRegs = append(inRegs, local)
		}
		if local, ok := localFromOperand(x.B); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{}, inRegs
	case *ir.NotEqualStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.A); ok {
			inRegs = append(inRegs, local)
		}
		if local, ok := localFromOperand(x.B); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{}, inRegs
	case *ir.IsArrayStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{}, inRegs
	case *ir.IsObjectStmt:
		inRegs := []ir.Local{}
		if local, ok := localFromOperand(x.Source); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{}, inRegs
	case *ir.IsDefinedStmt:
		return []ir.Local{}, []ir.Local{x.Source}
	case *ir.IsUndefinedStmt:
		return []ir.Local{}, []ir.Local{x.Source}
	case *ir.ArrayAppendStmt:
		inRegs := []ir.Local{x.Array}
		if local, ok := localFromOperand(x.Value); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Array}, inRegs
	case *ir.ObjectInsertStmt:
		inRegs := []ir.Local{x.Object}
		if local, ok := localFromOperand(x.Key); ok {
			inRegs = append(inRegs, local)
		}
		if local, ok := localFromOperand(x.Value); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Object}, inRegs
	case *ir.ObjectInsertOnceStmt:
		inRegs := []ir.Local{x.Object}
		if local, ok := localFromOperand(x.Key); ok {
			inRegs = append(inRegs, local)
		}
		if local, ok := localFromOperand(x.Value); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Object}, inRegs
	case *ir.ObjectMergeStmt:
		return []ir.Local{x.Target}, []ir.Local{x.A, x.B}
	case *ir.SetAddStmt:
		inRegs := []ir.Local{x.Set}
		if local, ok := localFromOperand(x.Value); ok {
			inRegs = append(inRegs, local)
		}
		return []ir.Local{x.Set}, inRegs
	case *ir.WithStmt:
		outRegs := []ir.Local{x.Local}
		inRegs := []ir.Local{x.Local}
		if local, ok := localFromOperand(x.Value); ok {
			inRegs = append(inRegs, local)
		}
		for _, stmt := range x.Block.Stmts {
			nestedOutRegs, nestedInRegs := extractLocalRefs(stmt)
			outRegs = append(outRegs, nestedOutRegs...)
			inRegs = append(inRegs, nestedInRegs...)
		}
		return outRegs, inRegs
	case *ir.NopStmt:
		return []ir.Local{}, []ir.Local{}
	case *ir.ResultSetAddStmt:
		return []ir.Local{}, []ir.Local{x.Value}
	default:
		return []ir.Local{}, []ir.Local{}
	}
}

type (
	UnsatMap map[ir.Local][]int // indexes of nodes needing that register.
	SatMap   map[ir.Local]int   // index of node providing a write to that register.
)

// Builds a graph of locals (registers) from a blocklist.
// offset provides the index offset in the overall instruction numbering.
func GetSatUnsatForBlocks(blocks []*ir.Block, offset int, unsatisfied UnsatMap, satisfied SatMap) (UnsatMap, SatMap) {
	for _, block := range blocks {
		unsatisfied, satisfied = GetSatUnsatForBlock(block, offset, unsatisfied, satisfied)
		offset += len(block.Stmts)
	}
	return unsatisfied, satisfied
}

// offset provides the index offset in the overall instruction numbering.
func GetSatUnsatForBlock(block *ir.Block, offset int, unsatisfied UnsatMap, satisfied SatMap) (UnsatMap, SatMap) {
	return GetSatUnsatForStmts(block.Stmts, offset, unsatisfied, satisfied)
}

// offset provides the index offset in the overall instruction numbering.
func GetSatUnsatForStmts(stmts []ir.Stmt, offset int, unsatisfied UnsatMap, satisfied SatMap) (UnsatMap, SatMap) {
	for idx, stmt := range stmts {
		outRegs, inRegs := extractLocalRefs(stmt)
		for _, local := range inRegs {
			// dependency is satisfied by an upstream instruction.
			if _, ok := satisfied[local]; ok {
				continue
			}
			// Otherwise, dependency is unsatisfied. Add to unsat list.
			unsatisfied[local] = append(unsatisfied[local], idx+offset)
		}
		// Update the sat set for downstream instructions.
		for _, local := range outRegs {
			satisfied[local] = idx + offset
		}
	}

	return unsatisfied, satisfied
}
