package ldap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const (
	Name = "ldap"
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	exit, doneExit chan struct{}

	transformRule ast.Ref
	transform     atomic.Pointer[rego.PreparedEvalQuery]
}

// Ensure that the kafka sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := c.prepareTransform(ctx); err != nil {
		return fmt.Errorf("prepare rego_transform: %w", err)
	}
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx) // Q: Does this context ever stop?
	return nil
}

func (c *Data) Stop(ctx context.Context) {
	if c.doneExit == nil {
		return
	}
	close(c.exit) // stops our polling loop
	select {
	case <-c.doneExit: // waits for polling loop to be stopped
	case <-ctx.Done(): // or exit if context canceled or timed out
	}
}

func (c *Data) Reconfigure(ctx context.Context, next any) {
	if c.Config.Equal(next.(Config)) {
		return // nothing to do
	}
	if c.doneExit != nil { // started before
		c.Stop(ctx)
	}
	c.Config = next.(Config)
	c.Start(ctx)
}

// dataPlugin accessors
func (c *Data) Name() string {
	return Name
}

func (c *Data) Path() storage.Path {
	return c.Config.path
}

func (c *Data) prepareTransform(ctx context.Context) error {
	return storage.Txn(ctx, c.manager.Store, storage.TransactionParams{}, func(txn storage.Transaction) error {
		return c.Trigger(ctx, txn)
	})
}

func (c *Data) Trigger(ctx context.Context, txn storage.Transaction) error {
	if c.Config.RegoTransformRule == "" {
		return nil
	}
	transformRef := ast.MustParseRef(c.Config.RegoTransformRule)
	// ref: data.x.transform => query: new = data.x.transform
	query := ast.NewBody(ast.Equality.Expr(ast.VarTerm("new"), ast.NewTerm(transformRef)))

	comp := c.manager.GetCompiler()
	if comp == nil || comp.RuleTree == nil || comp.RuleTree.Find(transformRef) == nil {
		c.manager.Logger().Warn("%s plugin (path %s): transform rule %q does not exist yet", c.Name(), c.Path(), transformRef)
		return nil
	}

	buf := bytes.Buffer{}
	r := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(comp),
		rego.Store(c.manager.Store),
		rego.Transaction(txn),
		rego.Runtime(c.manager.Info),
		rego.EnablePrintStatements(c.manager.EnablePrintStatements()),
		rego.PrintHook(topdown.NewPrintHook(&buf)),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}

	if buf.Len() > 0 {
		c.log.Debug("prepare print(): %s", buf.String())
	}
	c.transform.Store(&pq)
	return nil
}

func (c *Data) loop(ctx context.Context) {
	timer := time.NewTimer(0) // zero timer is needed to execute immediately for first time

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case <-c.exit:
			break LOOP
		case <-timer.C:
		}
		for _, u := range c.Config.urls { // try all dial urls
			select { // double check
			case <-ctx.Done():
				break LOOP
			case <-c.exit:
				break LOOP
			default:
			}
			err := c.poll(ctx, u)
			if err == nil {
				break
			}
			c.log.Error("polling for url %q failed: %+v", u.String(), err)
		}
		timer.Reset(c.Config.interval)
	}
	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
	close(c.doneExit)
}

func (c *Data) poll(ctx context.Context, u *url.URL) error {
	conn, err := getLDAPConn(ctx, u, c.Config.tls)
	if err != nil {
		return err
	}
	defer conn.Close()

	var controls []ldap.Control
	if c.Config.Username != "" {
		bindReq := ldap.NewSimpleBindRequest(c.Config.Username, c.Config.Password, nil)
		bindReq.AllowEmptyPassword = c.Config.Password == ""
		resp, err := conn.SimpleBind(bindReq)
		if err != nil {
			return fmt.Errorf("ldap bind operation failed: %+v", err)
		}
		controls = resp.Controls
	}

	req := ldap.NewSearchRequest(
		c.Config.BaseDN,
		c.Config.scope,
		c.Config.deref,
		0,
		0,
		false,
		c.Config.Filter,
		c.Config.attributes,
		controls,
	)
	result, err := conn.Search(req)
	if err != nil {
		var ldapErr *ldap.Error
		if !errors.As(err, &ldapErr) || ldapErr.ResultCode != ldap.LDAPResultSizeLimitExceeded {
			return fmt.Errorf("ldap search operation failed: %+v", err)
		}
	}
	if result == nil {
		c.log.Warn("ldap search returned empty result")
		return nil
	}

	data, err := convertEntries(result.Entries)
	if err != nil {
		return fmt.Errorf("converting result failed: %w", err)
	}
	txn, err := c.manager.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("create transaction: %w", err)
	}

	transformed := data
	if c.transformRule != nil {
		transformed, err = c.transformData(ctx, txn, data)
		if err != nil {
			c.manager.Store.Abort(ctx, txn)
			return fmt.Errorf("transform failed: %w", err)
		}
	}

	if err := inmem.WriteUncheckedTxn(ctx, c.manager.Store, txn, storage.ReplaceOp, c.Config.path, transformed); err != nil {
		c.manager.Store.Abort(ctx, txn)
		return fmt.Errorf("writing data to %+v failed: %v", c.Config.path, err)
	}
	return c.manager.Store.Commit(ctx, txn)
}

func convertEntries(entries []*ldap.Entry) (any, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	res := make([]any, len(entries))
	for i, entry := range entries {
		en, err := convertEntry(entry)
		if err != nil {
			return nil, err
		}
		res[i] = en
	}

	return res, nil
}

func convertEntry(entry *ldap.Entry) (any, error) {
	res := convertAttributes(entry.Attributes)

	dn, err := convertDN(entry.DN)
	if err != nil {
		return nil, err
	}
	res["dn"] = dn

	return res, nil
}

func convertAttributes(attributes []*ldap.EntryAttribute) map[string]any {
	if len(attributes) == 0 {
		return map[string]any{}
	}

	res := make(map[string]any, len(attributes))
	for _, attribute := range attributes {
		res[attribute.Name] = attribute.Values
	}

	return res
}

func convertDN(rawDN string) (map[string]any, error) {
	dn, err := ldap.ParseDN(rawDN)
	if err != nil {
		return nil, fmt.Errorf("could not parse DN %q: %w", rawDN, err)
	}

	res := make(map[string]any)
	for _, rdn := range dn.RDNs {
		for _, attribute := range rdn.Attributes {
			attr, ok := res[attribute.Type]
			if !ok {
				attr = []string{}
			}
			res[attribute.Type] = append(attr.([]string), attribute.Value)
		}
	}

	res["_raw"] = rawDN
	return res, nil
}

func (c *Data) transformData(ctx context.Context, txn storage.Transaction, incoming any) (any, error) {
	buf := &bytes.Buffer{}
	rs, err := c.transform.Load().Eval(ctx,
		rego.EvalInput(incoming),
		rego.EvalTransaction(txn),
		rego.EvalPrintHook(topdown.NewPrintHook(buf)),
	)
	if err != nil {
		return nil, err
	}
	if buf.Len() > 0 {
		c.log.Debug("rego_transform<%s>: %s", c.Config.RegoTransformRule, buf.String())
	}
	if len(rs) == 0 {
		c.log.Debug("incoming data discarded by transform: %v", incoming) // TODO(sr): this could be very large
		return nil, nil
	}
	return rs[0].Bindings["new"], nil
}
