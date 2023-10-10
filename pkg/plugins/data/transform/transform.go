package transform

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"

	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

type Rego struct {
	manager    *plugins.Manager
	name, path string
	rule       ast.Ref
	transform  atomic.Pointer[rego.PreparedEvalQuery]
}

// New instantiates the Rego transform. It'll be a no-op unless a reference
// to some Rego rule is provided (r0). The caller needs to assure that the ref
// string is a valid ast.Ref, otherwise this will panic.
func New(m *plugins.Manager, path, name, r0 string) *Rego {
	var r ast.Ref
	if r0 != "" {
		r = ast.MustParseRef(r0)
	}
	return &Rego{manager: m, name: name, path: path, rule: r}
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

func (s *Rego) Ready() bool {
	return s.transform.Load() != nil
}

// transformData applies Rego transform rule(s) to incoming, and returns the result.
func (s *Rego) transformData(ctx context.Context, txn storage.Transaction, incoming any) (any, error) {
	if s.rule == nil {
		return incoming, nil
	}
	buf := &bytes.Buffer{}
	rs, err := s.transform.Load().Eval(ctx,
		rego.EvalInput(incoming),
		rego.EvalTransaction(txn),
		rego.EvalPrintHook(topdown.NewPrintHook(buf)),
	)
	if err != nil {
		return nil, err
	}
	if buf.Len() > 0 {
		s.manager.Logger().Debug("rego_transform<%s>: %s", s.rule, buf.String())
	}
	if len(rs) == 0 || empty(rs[0].Bindings["new"]) {
		s.manager.Logger().Debug("incoming data discarded by transform: %v", incoming) // TODO(sr): this could be very large
		return nil, nil
	}
	return rs[0].Bindings["new"], nil
}

func empty(m any) bool {
	m0, ok := m.(map[string]any)
	return !ok || len(m0) == 0
}

func (s *Rego) Trigger(ctx context.Context, txn storage.Transaction) error {
	if s == nil || s.rule == nil {
		return nil
	}
	transformRef := s.rule
	// ref: data.x.transform => query: new = data.x.transform
	query := ast.NewBody(ast.Equality.Expr(ast.VarTerm("new"), ast.NewTerm(transformRef)))

	comp := s.manager.GetCompiler()
	// TODO(sr): improve our debugging story
	if comp == nil || comp.RuleTree == nil || comp.RuleTree.Find(transformRef) == nil {
		s.manager.Logger().Warn("%s plugin (data.%s): transform rule %q does not exist yet", s.name, s.path, transformRef)
		return nil
	}

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

// Ingest applies the transform rule(s) for this Rego object to incoming,
// and then commits the result to the store.
func (s *Rego) Ingest(ctx context.Context, path storage.Path, incoming any) error {
	txn, err := s.manager.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("create transaction: %w", err)
	}
	transformed := incoming
	if s != nil {
		transformed, err = s.transformData(ctx, txn, incoming)
		if err != nil {
			s.manager.Store.Abort(ctx, txn)
			return fmt.Errorf("transform failed: %w", err)
		}
	}
	if err := inmem.WriteUncheckedTxn(ctx, s.manager.Store, txn, storage.ReplaceOp, path, transformed); err != nil {
		return fmt.Errorf("writing data to %+v failed: %v", path, err)
	}
	return s.manager.Store.Commit(ctx, txn)
}
