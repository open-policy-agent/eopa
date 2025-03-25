package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
)

const (
	Name = "mongodb"
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
		// Never started
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
	defer close(c.doneExit)

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

		if err := c.poll(ctx, c.Config.URI, c.Config.credentials, c.Config.Database, c.Config.Collection, c.Config.Filter, c.Config.findOptions, c.Config.Canonical); err != nil {
			c.log.Error("polling for url %q failed: %+v", c.Config.URI, err)
		}

		timer.Reset(c.Config.interval)
	}

	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
}

func (c *Data) poll(ctx context.Context, uri string, credentials []byte, database string, collection string, filter interface{}, options *options.FindOptions, canonical bool) error {
	client, err := builtins.MongoDBClients.Get(ctx, uri, credentials)
	if err != nil {
		return err
	}

	cursor, err := client.Database(database).Collection(collection).Find(ctx, filter, options)
	if err != nil {
		return err
	}

	var docs []bson.M
	if err = cursor.All(ctx, &docs); err != nil {
		return err
	}

	root := make(map[string]interface{})

skip:
	for _, doc := range docs {
		data, err := bson.MarshalExtJSON(doc, canonical, false)
		if err != nil {
			return err
		}

		var result map[string]interface{}
		if err := util.UnmarshalJSON(data, &result); err != nil {
			return err
		}

		// Extract the primary key and remove it from the
		// stored document. If user used projection to filter
		// the _id out; can't do much but skip the row.  While
		// default MongoDB primary keys are UUID strings, it
		// does allow user-provided non-string id's. Those
		// primary keys we convert to strings.

		id, ok := result["_id"]
		if !ok {
			continue skip
		}

		delete(result, "_id")

		switch id := id.(type) {
		case string:
			root[id] = result
		default:
			encoded, err := json.Marshal(id)
			if err != nil {
				continue skip
			}

			root[string(encoded)] = result
		}
	}

	if err := c.Ingest(ctx, c.Path(), root); err != nil {
		return fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}
	return nil
}
