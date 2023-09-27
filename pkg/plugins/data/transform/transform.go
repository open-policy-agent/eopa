package transform

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
)

type Rego struct {
	manager   *plugins.Manager
	rule      ast.Ref
	transform atomic.Pointer[rego.PreparedEvalQuery]
}

func New(m *plugins.Manager, r ast.Ref) *Rego {
	return &Rego{manager: m, rule: r}
}

func Validate(r string) error {
	_, err := ast.ParseRef(r)
	if err != nil {
		return fmt.Errorf("invalid rego transform rule: %w", err)
	}
	return nil
}

func (s *Rego) Prepare(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return storage.Txn(ctx, s.manager.Store, storage.TransactionParams{}, func(txn storage.Transaction) error {
		return s.Trigger(ctx, txn)
	})
}

func (s *Rego) TransformData(ctx context.Context, txn storage.Transaction, incoming any) (any, string, error) {
	logs := strings.Builder{}
	buf := &bytes.Buffer{}
	rs, err := s.transform.Load().Eval(ctx,
		rego.EvalInput(incoming),
		rego.EvalTransaction(txn),
		rego.EvalPrintHook(topdown.NewPrintHook(buf)),
	)
	if err != nil {
		return nil, "", err
	}
	if buf.Len() > 0 {
		fmt.Fprintf(&logs, "rego_transform<%s>: %s", s.rule, buf.String())
	}
	if len(rs) == 0 {
		fmt.Fprintf(&logs, "incoming data discarded by transform: %v", incoming) // TODO(sr): this could be very large
		return nil, logs.String(), nil
	}
	return rs[0].Bindings["new"], logs.String(), nil
}

func (s *Rego) Trigger(ctx context.Context, txn storage.Transaction) error {
	if s == nil {
		return nil
	}
	transformRef := s.rule
	// ref: data.x.transform => query: new = data.x.transform
	query := ast.NewBody(ast.Equality.Expr(ast.VarTerm("new"), ast.NewTerm(transformRef)))

	comp := s.manager.GetCompiler()
	// TODO(sr): improve our debugging story
	// if comp == nil || comp.RuleTree == nil || comp.RuleTree.Find(transformRef) == nil {
	// 	s.manager.Logger().Warn("%s plugin (path %s): transform rule %q does not exist yet", c.Name(), c.Path(), transformRef)
	// 	return nil
	// }

	r := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(comp),
		rego.Store(s.manager.Store),
		rego.Transaction(txn),
		rego.Runtime(s.manager.Info),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}
	s.transform.Store(&pq)
	return nil
}
