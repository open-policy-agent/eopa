package decisionlogs

import (
	"context"

	"github.com/redpanda-data/benthos/v4/public/service"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
)

type MaskProcessor struct {
	prep     *rego.PreparedEvalQuery
	mgr      *plugins.Manager
	decision ast.Ref
}

func NewMask(pc *service.ParsedConfig, r *registerer) (*MaskProcessor, error) {
	m := &MaskProcessor{mgr: r.mgr}
	decision, _ := pc.FieldString("decision")
	ref, err := parseDataPath(decision)
	if err != nil {
		return nil, err
	}
	m.decision = ref

	r.register(m.update)
	return m, nil
}

func (r *MaskProcessor) Process(ctx context.Context, m *service.Message) (service.MessageBatch, error) {
	msg, err := m.AsStructured()
	if err != nil {
		return nil, err
	}

	res, err := r.mask(ctx, msg.(map[string]any))
	if err != nil {
		return nil, err
	}
	m.SetStructuredMut(res)
	return []*service.Message{m}, nil
}

func (*MaskProcessor) Close(context.Context) error {
	return nil
}

func (r *MaskProcessor) mask(ctx context.Context, ev map[string]any) (map[string]any, error) {
	if r.prep == nil {
		return ev, nil
	}
	rs, err := r.prep.Eval(ctx, rego.EvalInput(ev))
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return ev, nil
	}
	return maskEvent(rs[0].Expressions[0].Value, ev)
}

func (r *MaskProcessor) update(txn storage.Transaction) error {
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
