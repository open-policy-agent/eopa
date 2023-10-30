package main

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"git.sr.ht/~charles/graph"
	"github.com/open-policy-agent/opa/ir"
)

// ---------------------------------------------------------------------------
// Graph Types

// Start/End usable for Plans/Funcs alike.
type CFGDAG struct {
	G           *graph.Graph
	Start       graph.Node
	End         graph.Node
	BlockStarts []graph.Node
	BlockEnds   []graph.Node
}

type CFGDAGForest struct {
	Plans map[string]CFGDAG
	Funcs map[string]CFGDAG
}

type IRNodeData struct {
	Stmt ir.Stmt
	Data any
	Tags []string
}

type BlockListStartData struct {
	Data any
	Tags []string
}

type BlockListEndData struct {
	Data any
	Tags []string
}

type BlockStartData struct {
	Depth int
	Data  any
	Tags  []string
}

type BlockEndData struct {
	Depth int
	Data  any
	Tags  []string
}

type PlanStartData struct {
	Name string
	Data any
	Tags []string
}

type PlanEndData struct {
	Name string
	Data any
	Tags []string
}

type FuncStartData struct {
	Name string
	Data any
	Tags []string
}

type FuncEndData struct {
	Name string
	Data any
	Tags []string
}

type CallEdgeData struct{}

func NewIRNodeData(stmt ir.Stmt, data any, tags []string) IRNodeData {
	return IRNodeData{Stmt: stmt, Data: data, Tags: tags}
}

func newBlockStartData(depth int, data any, tags []string) BlockStartData {
	return BlockStartData{Depth: depth, Data: data, Tags: tags}
}

func newBlockEndData(depth int, data any, tags []string) BlockEndData {
	return BlockEndData{Depth: depth, Data: data, Tags: tags}
}

func newBlockListStartData(data any, tags []string) BlockListStartData {
	return BlockListStartData{Data: data, Tags: tags}
}

func newBlockListEndData(data any, tags []string) BlockListEndData {
	return BlockListEndData{Data: data, Tags: tags}
}

func (d IRNodeData) AsDOTLabel() string {
	return fmt.Sprintf("%s | %s", typeOfStmt(d.Stmt), strings.Join(argsOfStmt(d.Stmt), ", "))
}

func (d BlockListStartData) AsDOTLabel() string {
	return "BlockList Start"
}

func (d BlockListEndData) AsDOTLabel() string {
	return "BlockList End"
}

func (d BlockStartData) AsDOTLabel() string {
	return "Block Start"
}

func (d BlockEndData) AsDOTLabel() string {
	return "Block End"
}

func (d PlanStartData) AsDOTLabel() string {
	return "Plan Start"
}

func (d PlanEndData) AsDOTLabel() string {
	return "Plan End"
}

func (d FuncStartData) AsDOTLabel() string {
	return "Func Start"
}

func (d FuncEndData) AsDOTLabel() string {
	return "Func End"
}

// Render the entire graph as a forest of DAG's.
// We group the funcs and plans separately for visual clarity.
func (forest CFGDAGForest) AsDOT() string {
	out := strings.Builder{}
	// For each plan, recursively traverse the nodes/edges, and spit out DOT declarations for everything.
	// This will be ugly, but if it works, the graphs will be gorgeous.

	out.WriteString("digraph G {\n")
	// Create 1x subgraph per plan.
	i := 0
	for name, cfg := range forest.Plans {
		out.WriteString(cfg.AsDOT(name, "plan_"+strconv.FormatInt(int64(i), 10), "Plan "+name, 1))
		i++
	}
	i = 0
	for name, cfg := range forest.Funcs {
		out.WriteString(cfg.AsDOT(name, "func_"+strconv.FormatInt(int64(i), 10), "Func "+name, 1))
		i++
	}
	out.WriteString("}")
	return out.String()
}

func quote(s string) string {
	x := strconv.Quote(s)
	return x[1 : len(x)-1]
}

// We generate nodes with unique IDs: N_{plan,func}_$name_$blockIdx_$nodeID
func (g *CFGDAG) AsDOT(name, prefix, label string, indentLevel int) string {
	out := strings.Builder{}
	indentPrefix := strings.Repeat("\t", indentLevel)
	out.WriteString(indentPrefix + "subgraph \"cluster_" + prefix + "_" + quote(name) + "\" {\n")
	out.WriteString(indentPrefix + "label=\"" + quote(label) + "\"\n")
	descendants, err := EnumerateDescendants(g.Start)
	if err != nil {
		log.Fatal(err)
		return ""
	}
	for _, n := range descendants {
		out.WriteString(nodeAsDOT(n, prefix, indentLevel+1))
	}
	blockDepth := 0
	for _, n := range descendants {
		if data, err := n.Data(); err == nil {
			if data != nil {
				ownID := "N_" + prefix + "_" + strconv.FormatInt(n.ID(), 10)
				switch data.(type) {
				case BlockStartData:
					blockDepth++
					out.WriteString(strings.Repeat("\t", indentLevel+blockDepth))
					out.WriteString(fmt.Sprintf("subgraph \"cluster_block_%s\" {\n", ownID))
					out.WriteString(strings.Repeat("\t", indentLevel+blockDepth+1) + "label=\"\"\n")
					out.WriteString(strings.Repeat("\t", indentLevel+blockDepth+1) + "style=\"rounded\"\n")
				case BlockEndData:
					out.WriteString(strings.Repeat("\t", indentLevel+blockDepth))
					out.WriteString("}\n")
					blockDepth--
				}
			}
		}
		out.WriteString(nodeEdgesAsDOT(n, prefix, indentLevel+blockDepth+1))
	}
	out.WriteString(indentPrefix + "}\n")
	return out.String()
}

func nodeAsDOT(n graph.Node, prefix string, indentLevel int) string {
	out := strings.Repeat("\t", indentLevel)
	ownID := "\"N_" + prefix + "_" + strconv.FormatInt(n.ID(), 10) + "\""
	// for _, e := range outEdges {
	// 	// "Normal" edge
	// 	out += fmt.Sprintf("%s -> %s\n", ownID, "N"+strconv.FormatInt(e.MustSink().ID(), 10))
	// }
	if data, err := n.Data(); err == nil {
		if data != nil {
			switch x := data.(type) {
			case IRNodeData:
				out += fmt.Sprintf("%s [shape=\"record\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case BlockStartData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case BlockEndData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case BlockListStartData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case BlockListEndData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case PlanStartData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case PlanEndData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case FuncStartData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			case FuncEndData:
				out += fmt.Sprintf("%s [shape=\"rect\" label=\"%s\"];\n", ownID, x.AsDOTLabel())
			}
		} else {
			out += fmt.Sprintf("%s [label=\"--\"];\n", ownID)
		}
	}
	return out
}

func nodeEdgesAsDOT(n graph.Node, prefix string, indentLevel int) string {
	out := ""
	if outEdges, err := n.OutEdges(); err == nil && len(outEdges) > 0 {
		out += strings.Repeat("\t", indentLevel)
		ownID := "\"N_" + prefix + "_" + strconv.FormatInt(n.ID(), 10) + "\""
		for _, e := range outEdges {
			// "Normal" edge
			out += fmt.Sprintf("%s -> %s\n", ownID, "\"N_"+prefix+"_"+strconv.FormatInt(e.MustSink().ID(), 10)+"\"")
		}
	}
	return out
}

func operand2Str(op ir.Operand) string {
	if l, ok := op.Value.(*ir.Local); ok {
		return local2Str(*l)
	}
	s := op.Value.String()
	s = strings.Replace(s, "<", "(", -1)
	s = strings.Replace(s, ">", ")", -1)
	return s
}

func local2Str(l ir.Local) string {
	return strconv.FormatInt(int64(l), 10)
}

func argsOfStmt(stmt ir.Stmt) []string {
	switch x := stmt.(type) {
	case *ir.ReturnLocalStmt:
		return []string{local2Str(x.Source)}
	case *ir.CallStmt:
		args := make([]string, 0, len(x.Args))
		for _, op := range x.Args {
			args = append(args, operand2Str(op))
		}
		argStr := strings.Join(args, ", ")
		return []string{quote(x.Func), argStr, local2Str(x.Result)} // TODO: Deal with variable-length args better?
	case *ir.CallDynamicStmt:
		args := make([]string, 0, len(x.Args))
		for _, op := range x.Args {
			args = append(args, local2Str(op))
		}
		argStr := strings.Join(args, ", ")
		path := make([]string, 0, len(x.Path))
		for _, op := range x.Path {
			path = append(path, operand2Str(op))
		}
		pathStr := strings.Join(path, ", ")
		return []string{argStr, pathStr, local2Str(x.Result)}
	case *ir.BlockStmt:
		return []string{}
	case *ir.BreakStmt:
		return []string{strconv.FormatInt(int64(x.Index), 10)}
	case *ir.DotStmt:
		return []string{operand2Str(x.Source), local2Str(x.Target), operand2Str(x.Key)}
	case *ir.LenStmt:
		return []string{operand2Str(x.Source), local2Str(x.Target)}
	case *ir.ScanStmt:
		return []string{local2Str(x.Source), local2Str(x.Key), local2Str(x.Value)}
	case *ir.NotStmt:
		return []string{}
	case *ir.AssignIntStmt:
		return []string{strconv.FormatInt(x.Value, 10), local2Str(x.Target)}
	case *ir.AssignVarStmt:
		return []string{operand2Str(x.Source), local2Str(x.Target)}
	case *ir.AssignVarOnceStmt:
		return []string{operand2Str(x.Source), local2Str(x.Target)}
	case *ir.ResetLocalStmt:
		return []string{local2Str(x.Target)}
	case *ir.MakeNullStmt:
		return []string{local2Str(x.Target)}
	case *ir.MakeNumberIntStmt:
		return []string{strconv.FormatInt(x.Value, 10), local2Str(x.Target)}
	case *ir.MakeNumberRefStmt:
		return []string{strconv.FormatInt(int64(x.Index), 10), local2Str(x.Target)}
	case *ir.MakeArrayStmt:
		return []string{local2Str(x.Target)}
	case *ir.MakeObjectStmt:
		return []string{local2Str(x.Target)}
	case *ir.MakeSetStmt:
		return []string{local2Str(x.Target)}
	case *ir.EqualStmt:
		return []string{operand2Str(x.A), operand2Str(x.B)}
	case *ir.NotEqualStmt:
		return []string{operand2Str(x.A), operand2Str(x.B)}
	case *ir.IsArrayStmt:
		return []string{operand2Str(x.Source)}
	case *ir.IsObjectStmt:
		return []string{operand2Str(x.Source)}
	case *ir.IsDefinedStmt:
		return []string{local2Str(x.Source)}
	case *ir.IsUndefinedStmt:
		return []string{local2Str(x.Source)}
	case *ir.ArrayAppendStmt:
		return []string{operand2Str(x.Value), local2Str(x.Array)}
	case *ir.ObjectInsertStmt:
		return []string{operand2Str(x.Key), operand2Str(x.Value), local2Str(x.Object)}
	case *ir.ObjectInsertOnceStmt:
		return []string{operand2Str(x.Key), operand2Str(x.Value), local2Str(x.Object)}
	case *ir.ObjectMergeStmt:
		return []string{local2Str(x.A), local2Str(x.B), local2Str(x.Target)}
	case *ir.SetAddStmt:
		return []string{operand2Str(x.Value), local2Str(x.Set)}
	case *ir.WithStmt:
		path := make([]string, 0, len(x.Path))
		for _, op := range x.Path {
			path = append(path, strconv.FormatInt(int64(op), 10))
		}
		pathStr := strings.Join(path, ", ")
		return []string{local2Str(x.Local), pathStr, operand2Str(x.Value)}
	case *ir.NopStmt:
		return []string{}
	case *ir.ResultSetAddStmt:
		return []string{local2Str(x.Value)}
	default:
		return []string{}
	}
}

func typeOfStmt(stmt ir.Stmt) string {
	switch stmt.(type) {
	case *ir.ReturnLocalStmt:
		return "ReturnLocalStmt"
	case *ir.CallStmt:
		return "CallStmt"
	case *ir.CallDynamicStmt:
		return "CallDynamicStmt"
	case *ir.BlockStmt:
		return "BlockStmt"
	case *ir.BreakStmt:
		return "BreakStmt"
	case *ir.DotStmt:
		return "DotStmt"
	case *ir.LenStmt:
		return "LenStmt"
	case *ir.ScanStmt:
		return "ScanStmt"
	case *ir.NotStmt:
		return "NotStmt"
	case *ir.AssignIntStmt:
		return "AssignIntStmt"
	case *ir.AssignVarStmt:
		return "AssignVarStmt"
	case *ir.AssignVarOnceStmt:
		return "AssignVarOnceStmt"
	case *ir.ResetLocalStmt:
		return "ResetLocalStmt"
	case *ir.MakeNullStmt:
		return "MakeNullStmt"
	case *ir.MakeNumberIntStmt:
		return "MakeNumberIntStmt"
	case *ir.MakeNumberRefStmt:
		return "MakeNumberRefStmt"
	case *ir.MakeArrayStmt:
		return "MakeArrayStmt"
	case *ir.MakeObjectStmt:
		return "MakeObjectStmt"
	case *ir.MakeSetStmt:
		return "MakeSetStmt"
	case *ir.EqualStmt:
		return "EqualStmt"
	case *ir.NotEqualStmt:
		return "NotEqualStmt"
	case *ir.IsArrayStmt:
		return "IsArrayStmt"
	case *ir.IsObjectStmt:
		return "IsObjectStmt"
	case *ir.IsDefinedStmt:
		return "IsDefinedStmt"
	case *ir.IsUndefinedStmt:
		return "IsUndefinedStmt"
	case *ir.ArrayAppendStmt:
		return "ArrayAppendStmt"
	case *ir.ObjectInsertStmt:
		return "ObjectInsertStmt"
	case *ir.ObjectInsertOnceStmt:
		return "ObjectInsertOnceStmt"
	case *ir.ObjectMergeStmt:
		return "ObjectMergeStmt"
	case *ir.SetAddStmt:
		return "SetAddStmt"
	case *ir.WithStmt:
		return "WithStmt"
	case *ir.NopStmt:
		return "NopStmt"
	case *ir.ResultSetAddStmt:
		return "ResultSetAddStmt"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Graph Types

// A descendant exists where some path exists from the root node to the descendent node.
func EnumerateDescendants(root graph.Node) ([]graph.Node, error) {
	out := []graph.Node{}
	if err := root.ForEachBFS(func(n graph.Node) bool {
		out = append(out, n)
		return true
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// Creates a graph node with the IR statement referenced from it.
func IRNodeFromStmt(g *graph.Graph, stmt ir.Stmt) graph.Node {
	nodeData := NewIRNodeData(stmt, nil, nil)
	return g.NewNodeWithData(nodeData)
}

// Checks to see if source has sink as an immediate successor node, with no neighbors in the path between them.
func HasImmediateSuccessor(source, sink graph.Node) bool {
	hasSuccessor := false
	source.ForeachSuccessor(func(n graph.Node) {
		if n.ID() == sink.ID() {
			hasSuccessor = true
		}
	})
	return hasSuccessor
}

// Checks to see if nodes are adjacent, and have only one edge between them.
func IsPairStitchable(g *graph.Graph, source, sink graph.Node) bool {
	ok := false
	if HasImmediateSuccessor(source, sink) {
		// Ensure only single path exists here.
		if es, err := g.EdgesBetween(source, sink); err == nil && len(es) == 1 {
			ok = true
		}
	}
	return ok
}

// Inserts the newcomer Node into the graph, and then stitches up the DAG relationship between the before and after nodes.
// Returns whether or not the node was successfully inserted.
// Warning: All nodes MUST be in the graph already.
// Note: These nodes had to be previously connected by an edge for this insertion to work.
func InsertAndStitchNode(g *graph.Graph, before, after, newcomer graph.Node) error {
	if IsPairStitchable(g, before, after) {
		// Get the edge, and then replace the it with 2x edges and add the newcomer node.
		es, _ := g.EdgesBetween(before, after)
		oldEdge := es[0]
		oldEdge.Delete()
		if _, err := g.NewEdgeWithData(before, newcomer, nil); err != nil {
			return err
		}
		if _, err := g.NewEdgeWithData(newcomer, after, nil); err != nil {
			return err
		}
		return nil
	}
	return errors.New("could not stitch nodes together")
}

// A more efficient way to string together all of the nodes in a function.
// Note: before and after nodes must already exist.
func StitchNodesForFunc(g *graph.Graph, before, after graph.Node, f *ir.Func) error {
	// Walk through all of the nodes in the block, recursing when needed.
	// Goal is to be ugly-but-effective. Ugh.
	prevNode := before
	for _, block := range f.Blocks {
		blockStart := g.NewNodeWithData(newBlockStartData(1, nil, nil))
		blockEnd := g.NewNodeWithData(newBlockEndData(1, nil, nil))
		// Stitch block start to parent.
		if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
			return err
		}
		if err := StitchNodesForBlock(g, []graph.Node{before, blockStart}, []graph.Node{after, blockEnd}, 1, block); err != nil {
			return err
		}
		prevNode = blockEnd
	}
	// Manually stitch up the last stmt/node in the block to the 'after' node.
	if _, err := g.NewEdgeWithData(prevNode, after, nil); err != nil {
		return err
	}
	return nil
}

// A more efficient way to string together all of the nodes in a plan.
// Note: before and after nodes must already exist.
func StitchNodesForPlan(g *graph.Graph, before, after graph.Node, p *ir.Plan) error {
	// Walk through all of the nodes in the block, recursing when needed.
	// Goal is to be ugly-but-effective. Ugh.
	prevNode := before
	for _, block := range p.Blocks {
		blockStart := g.NewNodeWithData(newBlockStartData(1, nil, nil))
		blockEnd := g.NewNodeWithData(newBlockEndData(1, nil, nil))
		// Stitch block start to parent.
		if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
			return err
		}
		if err := StitchNodesForBlock(g, []graph.Node{before, blockStart}, []graph.Node{after, blockEnd}, 1, block); err != nil {
			return err
		}
		prevNode = blockEnd
	}
	// Manually stitch up the last stmt/node in the block to the 'after' node.
	if _, err := g.NewEdgeWithData(prevNode, after, nil); err != nil {
		return err
	}
	return nil
}

// A more efficient way to string together all of the nodes in a block.
// Note: before and after nodes must already exist.
// NOTE: before and after represent *stacks* of before/after targets, allowing edge creation to *arbitrary* parent before/after targets.
// Creates: All of the nodes needed for the block, and its recursive descendants.
func StitchNodesForBlock(g *graph.Graph, before, after []graph.Node, depth int, block *ir.Block) error {
	if len(before) != len(after) {
		panic(fmt.Sprintf("Before/After stack length mismatch: %d vs %d", len(before), len(after)))
	}
	if len(before) == 0 {
		panic("Before stack is empty")
	}
	if len(after) == 0 {
		panic("After stack is empty")
	}
	// Walk through all of the nodes in the block, recursing when needed.
	// Goal is to be ugly-but-effective. Ugh.
	prevNode := before[len(before)-1]
	for _, stmt := range block.Stmts {
		sNode := IRNodeFromStmt(g, stmt)
		if _, err := g.NewEdgeWithData(prevNode, sNode, nil); err != nil {
			return err
		}
		prevNode = sNode
		// Special-case the nodes that require recursion:
		switch x := stmt.(type) {
		case *ir.BlockStmt:
			for _, b := range x.Blocks {
				blockStart := g.NewNodeWithData(newBlockStartData(depth+1, nil, nil))
				blockEnd := g.NewNodeWithData(newBlockEndData(depth+1, nil, nil))
				// Stitch block start to parent.
				if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
					return err
				}
				// Recurse into child block:
				if err := StitchNodesForBlock(g, append(before, blockStart), append(after, blockEnd), depth+1, b); err != nil {
					return err
				}
				prevNode = blockEnd
			}
		case *ir.NotStmt:
			blockStart := g.NewNodeWithData(newBlockStartData(depth+1, nil, nil))
			blockEnd := g.NewNodeWithData(newBlockEndData(depth+1, nil, nil))
			// Stitch block start to parent.
			if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
				return err
			}
			// Recurse into child block:
			if err := StitchNodesForBlock(g, append(before, blockStart), append(after, blockEnd), depth+1, x.Block); err != nil {
				return err
			}
			prevNode = blockEnd
		case *ir.ScanStmt:
			blockStart := g.NewNodeWithData(newBlockStartData(depth+1, nil, nil))
			blockEnd := g.NewNodeWithData(newBlockEndData(depth+1, nil, nil))
			// Stitch block start to parent.
			if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
				return err
			}
			// Recurse into child block:
			if err := StitchNodesForBlock(g, append(before, blockStart), append(after, blockEnd), depth+1, x.Block); err != nil {
				return err
			}
			prevNode = blockEnd
		case *ir.WithStmt:
			blockStart := g.NewNodeWithData(newBlockStartData(depth+1, nil, nil))
			blockEnd := g.NewNodeWithData(newBlockEndData(depth+1, nil, nil))
			// Stitch block start to parent.
			if _, err := g.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
				return err
			}
			// Recurse into child block:
			if err := StitchNodesForBlock(g, append(before, blockStart), append(after, blockEnd), depth+1, x.Block); err != nil {
				return err
			}
			prevNode = blockEnd
		}
	}
	// Manually stitch up the last stmt/node in the block to the 'after' node.
	if _, err := g.NewEdgeWithData(prevNode, after[len(before)-1], nil); err != nil {
		return err
	}
	return nil
}

// Removes the "leaver" node from the graph, then stitches up the DAG relationship between before and after nodes in its wake.
func RemoveAndStitchNode(g *graph.Graph, before, after, leaver graph.Node) error {
	if IsPairStitchable(g, before, leaver) && IsPairStitchable(g, leaver, after) {
		// Get the edges stitching the leaving node into the graph, and delete them.
		esBefore, _ := g.EdgesBetween(before, leaver)
		esAfter, _ := g.EdgesBetween(leaver, after)
		edgeBefore := esBefore[0]
		edgeBefore.Delete()
		edgeAfter := esAfter[0]
		edgeAfter.Delete()
		// Stitch up before and after nodes with a new edge between them.
		if _, err := g.NewEdgeWithData(before, after, nil); err != nil {
			return err
		}
		return nil
	}
	return errors.New("could not unstitch node")
}

func PlanToCFGDAG(plan *ir.Plan) (CFGDAG, error) {
	out := CFGDAG{}
	out.G = graph.NewGraph()
	out.Start = out.G.NewNodeWithData(newBlockListStartData(nil, nil))
	out.End = out.G.NewNodeWithData(newBlockListEndData(nil, nil))
	prevNode := out.Start
	for _, block := range plan.Blocks {
		blockStart := out.G.NewNodeWithData(newBlockStartData(0, nil, nil))
		blockEnd := out.G.NewNodeWithData(newBlockEndData(0, nil, nil))
		if _, err := out.G.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
			return CFGDAG{}, err
		}
		StitchNodesForBlock(out.G, []graph.Node{blockStart}, []graph.Node{blockEnd}, 1, block)
		out.BlockStarts = append(out.BlockStarts, blockStart)
		out.BlockEnds = append(out.BlockEnds, blockEnd)
		prevNode = blockEnd
	}
	// Manually stitch up the last stmt/node in the block to the 'after' node.
	if _, err := out.G.NewEdgeWithData(prevNode, out.End, nil); err != nil {
		return CFGDAG{}, err
	}
	return out, nil
}

func FuncToCFGDAG(fun *ir.Func) (CFGDAG, error) {
	out := CFGDAG{}
	out.G = graph.NewGraph()
	out.Start = out.G.NewNodeWithData(newBlockListStartData(nil, nil))
	out.End = out.G.NewNodeWithData(newBlockListEndData(nil, nil))
	prevNode := out.Start
	for _, block := range fun.Blocks {
		blockStart := out.G.NewNodeWithData(newBlockStartData(0, nil, nil))
		blockEnd := out.G.NewNodeWithData(newBlockEndData(0, nil, nil))
		if _, err := out.G.NewEdgeWithData(prevNode, blockStart, nil); err != nil {
			return CFGDAG{}, err
		}
		StitchNodesForBlock(out.G, []graph.Node{blockStart}, []graph.Node{blockEnd}, 1, block)
		out.BlockStarts = append(out.BlockStarts, blockStart)
		out.BlockEnds = append(out.BlockEnds, blockEnd)
		prevNode = blockEnd
	}
	// Manually stitch up the last stmt/node in the block to the 'after' node.
	if _, err := out.G.NewEdgeWithData(prevNode, out.End, nil); err != nil {
		return CFGDAG{}, err
	}
	return out, nil
}

func PolicyToCFGDAGForest(policy *ir.Policy) (CFGDAGForest, error) {
	out := CFGDAGForest{
		Plans: make(map[string]CFGDAG, len(policy.Plans.Plans)),
		Funcs: make(map[string]CFGDAG, len(policy.Funcs.Funcs)),
	}
	for _, p := range policy.Plans.Plans {
		name := p.Name
		cfg, err := PlanToCFGDAG(p)
		if err != nil {
			return CFGDAGForest{}, err
		}
		out.Plans[name] = cfg
	}
	for _, f := range policy.Funcs.Funcs {
		name := f.Name
		cfg, err := FuncToCFGDAG(f)
		if err != nil {
			return CFGDAGForest{}, err
		}
		out.Funcs[name] = cfg
	}
	return out, nil
}
