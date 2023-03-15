package data

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"go.uber.org/goleak"

	"github.com/styrainc/load-private/pkg/plugins/data/kafka"
	inmem "github.com/styrainc/load-private/pkg/store"
)

func TestKafkaReconfigure(t *testing.T) {
	t.Run("change path", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		// When the subtree path is changed, stop the plugin and start it with the new location
		// NOTE(sr): We could try to do better than that, but then we'd need to keep track of
		// the plugin state over multiple instances: we'd copy the data into a new store path,
		// and inform the plugin about the new location.
		mgr := getTestManager()
		config := `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`
		c, err := Factory().Validate(mgr, []byte(config))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		dp := Factory().New(mgr, c)
		ctx := context.Background()
		if err := dp.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer dp.Stop(ctx)

		next := Config{DataPlugins: make(map[string]DataPlugin)}
		next.DataPlugins["kafka.new.updates"] = c.(Config).DataPlugins["kafka.updates"]

		dp.Reconfigure(ctx, next)

		{ // check internal config, added key:
			exp, act := next.DataPlugins["kafka.new.updates"].Config.(kafka.Config), dp.(*Data).plugins["kafka.new.updates"].(*kafka.Data).Config
			if diff := cmp.Diff(exp, act, cmpopts.IgnoreUnexported(kafka.Config{})); diff != "" {
				t.Error("expected new config to be set, got (-want +got):\n", diff)
			}
		}
		{ // removed key:
			act, ok := dp.(*Data).plugins["kafka.updates"]
			if ok {
				t.Errorf("expected old config to be removed: %v", act)
			}
		}
	})

	t.Run("change config, keep path", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		mgr := getTestManager()
		config := `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`
		c, err := Factory().Validate(mgr, []byte(config))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		dp := Factory().New(mgr, c)
		ctx := context.Background()
		if err := dp.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer dp.Stop(ctx)

		// copy old config, change URLs
		next := Config{DataPlugins: make(map[string]DataPlugin)}
		next.DataPlugins["kafka.updates"] = c.(Config).DataPlugins["kafka.updates"]
		nextKafka := next.DataPlugins["kafka.updates"].Config.(kafka.Config)
		nextKafka.URLs = []string{"foo:9092"}
		next.DataPlugins["kafka.updates"] = DataPlugin{
			Factory: c.(Config).DataPlugins["kafka.updates"].Factory,
			Config:  nextKafka,
		}
		dp.Reconfigure(ctx, next)

		// check internal config
		exp, act := nextKafka, dp.(*Data).plugins["kafka.updates"].(*kafka.Data).Config
		if diff := cmp.Diff(exp, act, cmpopts.IgnoreUnexported(kafka.Config{})); diff != "" {
			t.Error("expected new config to be set, got (-want +got):\n", diff)
		}
	})

	t.Run("removing plugin removes data", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		mgr := getTestManager()
		config := `
kafka.updates:
  type: kafka
  urls: [127.0.0.1:8083]
  topics: [updates]
  rego_transform: data.utils.transform_events
`
		c, err := Factory().Validate(mgr, []byte(config))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		dp := Factory().New(mgr, c)
		ctx := context.Background()

		// setup some extra data
		if err := inmem.WriteUnchecked(ctx, mgr.Store, storage.AddOp, []string{}, map[string]any{"something": map[string]any{"else": true}}); err != nil {
			t.Fatalf("Setup store: %v", err)
		}

		if err := dp.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer dp.Stop(ctx)

		if err := inmem.WriteUnchecked(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/kafka/updates"), map[string]any{"foo": "bar"}); err != nil {
			t.Fatalf("Setup store: %v", err)
		}
		prev, err := storage.ReadOne(ctx, mgr.Store, []string{})
		if err != nil {
			t.Fatal(err)
		}

		next := Config{DataPlugins: make(map[string]DataPlugin)} // no plugins
		dp.Reconfigure(ctx, next)

		last, err := storage.ReadOne(ctx, mgr.Store, []string{})
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(prev, last); diff == "" {
			t.Errorf("Expected prev!=last, got the same: %v", last)
		}

		act, err := storage.ReadOne(ctx, mgr.Store, []string{"kafka"})
		if !storage.IsNotFound(err) {
			t.Errorf("expected empty tree path to be removed, found %v", act)
		}
	})

	t.Run("removing plugin removes data but keeps tree intact if there is other data", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		mgr := getTestManager()
		config := `
kafka.updates:
  type: kafka
  urls: [127.0.0.1:8083]
  topics: [updates]
  rego_transform: data.utils.transform_events
`
		c, err := Factory().Validate(mgr, []byte(config))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		dp := Factory().New(mgr, c)
		ctx := context.Background()

		// setup some extra data
		if err := inmem.WriteUnchecked(ctx, mgr.Store, storage.AddOp, []string{}, map[string]any{"kafka": map[string]any{"else": true}}); err != nil {
			t.Fatalf("Setup store: %v", err)
		}

		if err := dp.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer dp.Stop(ctx)
		if err := inmem.WriteUnchecked(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/kafka/updates"), map[string]any{"foo": "bar"}); err != nil {
			t.Fatalf("Setup store: %v", err)
		}
		prev, err := storage.ReadOne(ctx, mgr.Store, []string{})
		if err != nil {
			t.Fatal(err)
		}

		next := Config{DataPlugins: make(map[string]DataPlugin)} // no plugins
		dp.Reconfigure(ctx, next)

		last, err := storage.ReadOne(ctx, mgr.Store, []string{})
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(prev, last); diff == "" {
			t.Errorf("Expected prev!=last, got the same: %v", last)
		}

		act, err := storage.ReadOne(ctx, mgr.Store, []string{"kafka"})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(act.(map[string]any), map[string]any{"else": true}); diff != "" {
			t.Error("unexpected data: (-want, +got):\n", diff)
		}
	})
}

func getTestManager() *plugins.Manager {
	return getTestManagerWithOpts(nil)
}

func getTestManagerWithOpts(config []byte, stores ...storage.Store) *plugins.Manager {
	store := inmem.New()
	if len(stores) == 1 {
		store = stores[0]
	}

	manager, err := plugins.New(config, "test-instance-id", store)
	if err != nil {
		panic(err)
	}
	return manager
}
