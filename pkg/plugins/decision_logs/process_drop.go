package decisionlogs

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
)

type DropProcessor struct {
	prep     *rego.PreparedEvalQuery
	mgr      *plugins.Manager
	decision ast.Ref
}

func NewDrop(pc *service.ParsedConfig, r *registerer) (*DropProcessor, error) {
	d := &DropProcessor{mgr: r.mgr}
	decision, _ := pc.FieldString("decision")
	ref, err := parseDataPath(decision)
	if err != nil {
		return nil, err
	}
	d.decision = ref

	r.register(d.update)
	return d, nil
}

func (r *DropProcessor) Process(ctx context.Context, m *service.Message) (service.MessageBatch, error) {
	msg, err := m.AsStructured()
	if err != nil {
		return nil, err
	}

	drop, err := r.drop(ctx, msg.(map[string]any))
	switch {
	case err != nil:
		return nil, err
	case drop:
		return nil, nil
	default:
		return []*service.Message{m}, nil
	}
}

func (*DropProcessor) Close(context.Context) error {
	return nil
}

func (r *DropProcessor) drop(ctx context.Context, ev map[string]any) (bool, error) {
	if r.prep == nil {
		return false, nil
	}
	rs, err := r.prep.Eval(ctx, rego.EvalInput(ev))
	return rs.Allowed(), err
}

func (r *DropProcessor) update(txn storage.Transaction) error {
	query := ast.NewBody(ast.NewExpr(ast.NewTerm(r.decision)))
	st := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(r.mgr.GetCompiler()),
		rego.Store(r.mgr.Store),
		rego.Transaction(txn),
		rego.Runtime(r.mgr.Info),
		rego.EnablePrintStatements(r.mgr.EnablePrintStatements()),
		rego.PrintHook(r.mgr.PrintHook()),
	)

	pq, err := st.PrepareForEval(context.TODO())
	if err != nil {
		return err
	}
	r.prep = &pq
	return nil
}
