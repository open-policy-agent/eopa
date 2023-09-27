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
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestMongoDB(t *testing.T) {
	username, password := "root", "password"
	mongodb, uri := startMongoDB(t, username, password)
	defer mongodb.Terminate(context.Background())

	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)

	for _, tt := range []struct {
		name     string
		config   string
		expected any
		ignoreID bool
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
      keys: ["foo"]
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{"foo": "a", "bar": json.Number("0")},
				"b": map[string]interface{}{"foo": "b", "bar": json.Number("1")},
			},
			ignoreID: true,
		},
		{
			name: "two keys",
			config: `
plugins:
  data:
    mongodb.placeholder:
      type: mongodb
      uri: URI
      auth: AUTH
      database: database
      collection: collection2
      keys: ["foo", "bar"]
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{
					"c": map[string]interface{}{"foo": "a", "bar": "c"},
					"d": map[string]interface{}{"foo": "a", "bar": "d"},
				},
			},
			ignoreID: true,
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
      keys: ["foo"]
      filter: {"bar": 0}
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{"foo": "a", "bar": json.Number("0")},
			},
			ignoreID: true,
		},
		{
			name: "options",
			config: `
plugins:
  data:
    mongodb.placeholder:
      type: mongodb
      uri: URI
      auth: AUTH
      database: database
      collection: collection1
      keys: ["foo"]
      find_options: {"projection": {"_id": false}}
`,
			expected: map[string]interface{}{
				"a": map[string]interface{}{"foo": "a", "bar": json.Number("0")},
				"b": map[string]interface{}{"foo": "b", "bar": json.Number("1")},
			},
			ignoreID: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
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

			var options []cmp.Option
			if tt.ignoreID {
				options = append(options, cmp.FilterPath(func(p cmp.Path) bool {
					return strings.HasSuffix(p.GoString(), `["_id"]`)
				}, cmp.Ignore()))
			}

			if diff := cmp.Diff(tt.expected, act, options...); diff != "" {
				t.Errorf("data value mismatch, diff:%s", diff)
			}
		})
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
			Image:        "mongo:6",
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
		Logger:  testcontainers.TestLogger(t),
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := container.Endpoint(ctx, "mongodb")
	if err != nil {
		t.Fatal(err)
	}

	// Create the test content.
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(authMongoURI(endpoint, username, password)))
	if err != nil {
		t.Fatal(err)
	}

	collection := client.Database("database").Collection("collection1")

	if _, err := collection.InsertOne(ctx, bson.D{{Key: "foo", Value: "a"}, {Key: "bar", Value: 0}}); err != nil {
		t.Fatal(err)
	}

	if _, err := collection.InsertOne(ctx, bson.D{{Key: "foo", Value: "b"}, {Key: "bar", Value: 1}}); err != nil {
		t.Fatal(err)
	}

	collection = client.Database("database").Collection("collection2")

	if _, err := collection.InsertOne(ctx, bson.D{{Key: "foo", Value: "a"}, {Key: "bar", Value: "c"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := collection.InsertOne(ctx, bson.D{{Key: "foo", Value: "a"}, {Key: "bar", Value: "d"}}); err != nil {
		t.Fatal(err)
	}

	return container, endpoint
}

func authMongoURI(uri string, username string, password string) string {
	return strings.ReplaceAll(uri, "localhost", username+":"+password+"@localhost")
}
