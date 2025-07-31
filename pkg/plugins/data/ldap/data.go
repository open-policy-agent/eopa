// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package ldap

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"

	"github.com/open-policy-agent/eopa/pkg/plugins/data/transform"
	"github.com/open-policy-agent/eopa/pkg/plugins/data/types"
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

	*transform.Rego
}

// Ensure that this sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := c.Prepare(ctx); err != nil {
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

	if err := c.Ingest(ctx, c.Path(), data); err != nil {
		return fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}
	return nil
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
