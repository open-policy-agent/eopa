package mongodb_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go"
	tc_log "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/discovery"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/open-policy-agent/eopa/pkg/plugins/data"
	inmem "github.com/open-policy-agent/eopa/pkg/storage"
)

const username, password = "root", "password"

func TestMongoDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mongodb, uri := startMongoDB(t, username, password)
	t.Cleanup(func() { mongodb.Terminate(ctx) })
	createCollections(ctx, t, uri)

	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)

	for _, tt := range []struct {
		name     string
		config   string
		expected any
	}{
		{
			name: "one key",
			config: `
plugins:
  data:
    mongodb.placeholder:
      type: mongodb
      uri: URI
      auth: AUTH
      database: database
      collection: collection1
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{"bar": json.Number("0")},
				"b": map[string]interface{}{"bar": json.Number("1")},
			},
		},
		{
			name: "filter",
			config: `
plugins:
  data:
    mongodb.placeholder:
      type: mongodb
      uri: URI
      auth: AUTH
      database: database
      collection: collection1
      filter: {"bar": 0}
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{"bar": json.Number("0")},
			},
		},
		{
			name: "options (exclude bar field)",
			config: `
plugins:
  data:
    mongodb.placeholder:
      type: mongodb
      uri: URI
      auth: AUTH
      database: database
      collection: collection1
      find_options: {"projection": {"bar": 0}}
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{},
				"b": map[string]interface{}{},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := inmem.New()
			mgr := pluginMgr(t, store, strings.ReplaceAll(strings.ReplaceAll(tt.config, "URI", uri), "AUTH", auth))

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			waitForStorePath(ctx, t, store, "/mongodb/placeholder")
			act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/mongodb/placeholder"))
			if err != nil {
				t.Fatalf("read back data: %v", err)
			}

			if diff := cmp.Diff(tt.expected, act); diff != "" {
				t.Errorf("data value mismatch, diff:%s", diff)
			}
		})
	}
}

func TestRegoTransform(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tx, uri := startMongoDB(t, username, password)
	t.Cleanup(func() { tx.Terminate(ctx) })

	// TODO(sr): create some realistic-looking data instead?
	createCollections(ctx, t, uri)

	config := `
plugins:
  data:
    entities: # arbitrary!
      type: mongodb
      uri: %[1]s
      auth: %[2]s
      database: database
      collection: collection1
      keys: ["foo"]
      rego_transform: data.e2e.transform
`
	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)

	transform := `package e2e
transform[key] := blob.bar if some key, blob in input.incoming
`

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(t, store, fmt.Sprintf(config, uri, auth))
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/entities")
	act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/entities"))
	if err != nil {
		t.Fatalf("read back data: %v", err)
	}

	exp := map[string]any{
		"a": json.Number("0"),
		"b": json.Number("1"),
	}
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("data value mismatch, diff:\n%s", diff)
	}
}

func pluginMgr(t *testing.T, store storage.Store, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{data.Name: data.Factory()}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

func waitForStorePath(ctx context.Context, t *testing.T, store storage.Store, path string) {
	t.Helper()
	if err := util.WaitFunc(func() bool {
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath(path))
		if err != nil {
			if storage.IsNotFound(err) {
				return false
			}
			t.Fatalf("read back data: %v", err)
		}
		if cmp.Diff(map[string]any{}, act) == "" { // empty obj
			return false
		}
		return true
	}, 200*time.Millisecond, 30*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}

func startMongoDB(t *testing.T, username, password string) (testcontainers.Container, string) {
	t.Helper()

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mongo:8",
			ExposedPorts: []string{"27017/tcp"},
			Env: map[string]string{
				"MONGO_INITDB_ROOT_USERNAME": username,
				"MONGO_INITDB_ROOT_PASSWORD": password,
			},

			WaitingFor: wait.ForAll(
				wait.ForLog("Waiting for connections"),
				wait.ForListeningPort("27017/tcp"),
			),
		},
		Logger:  tc_log.TestLogger(t),
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := container.Endpoint(ctx, "mongodb")
	if err != nil {
		t.Fatal(err)
	}
	return container, endpoint
}

func createCollections(ctx context.Context, t *testing.T, endpoint string) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(authMongoURI(endpoint, username, password)))
	if err != nil {
		t.Fatal(err)
	}

	collection := client.Database("database").Collection("collection1")
	if _, err := collection.InsertOne(ctx, bson.D{{Key: "_id", Value: "a"}, {Key: "bar", Value: 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collection.InsertOne(ctx, bson.D{{Key: "_id", Value: "b"}, {Key: "bar", Value: 1}}); err != nil {
		t.Fatal(err)
	}
}

func authMongoURI(uri string, username string, password string) string {
	return strings.ReplaceAll(uri, "localhost", username+":"+password+"@localhost")
}

func storeWithPolicy(ctx context.Context, t *testing.T, transform string) storage.Store {
	t.Helper()
	store := inmem.New()
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}
	return store
}
