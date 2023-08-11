package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestMongoDBSend(t *testing.T) {
	username, password := "root", "password"
	mongodb, uri := startMongoDB(t, username, password)
	defer mongodb.Terminate(context.Background())

	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)
	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
	}{
		{
			"missing parameter(s)",
			`p := mongodb.send({})`,
			"",
			`eval_type_error: mongodb.send: operand 1 missing required request parameters(s): {"uri"}`,
			false,
			now,
			0,
		},
		{
			"non-existing doc query (find many)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "nonexisting"}}})`, uri, auth),
			`{{"result": {"p": {}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"a single row query (find many)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"a single row query (find many, canonical)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}, "canonical": true}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": {"$numberInt": "1"}, "foo": "x"}]}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"non-existing doc query (find one)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find_one": {"database": "database", "collection": "collection", "filter": {"foo": "nonexisting"}}})`, uri, auth),
			`{{"result": {"p": {}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"a single row query (find one)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find_one": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"document": {"bar": 1, "foo": "x"}}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"a single row query (find one, canonical)",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "auth": %s, "find_one": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}, "canonical": true}})`, uri, auth),
			`{{"result": {"p": {"document": {"bar": {"$numberInt": "1"}, "foo": "x"}}}}}`,
			"",
			false,
			now,
			0,
		},

		{
			"intra-query query cache",
			fmt.Sprintf(`p = [ resp1, resp2 ] {
                                mongodb.send({"uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}}, resp1)
                                mongodb.send({"uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}}, resp2) # cached
				}`, uri, auth, uri, auth),
			`{{"result": {"p": [{"documents": [{"bar": 1, "foo": "x"}]}, {"documents": [{"bar": 1, "foo": "x"}]}]}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache warmup (default duration)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "uri": "%s", "auth" :%s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache check (default duration, valid)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault - 1),
			1,
		},
		{
			"inter-query query cache check (default duration, expired)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault),
			0,
		},
		{
			"inter-query query cache warmup (explicit duration)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache check (explicit duration, valid)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,

			"",
			true, // keep the warmup results
			now.Add(10*time.Second - 1),
			1,
		},
		{
			"inter-query query cache check (explicit duration, expired)",
			fmt.Sprintf(`p := mongodb.send({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"Projection": {"_id": false}}}})`, uri, auth),
			`{{"result": {"p": {"documents": [{"bar": 1, "foo": "x"}]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(10 * time.Second),
			0,
		},
		{
			"error w/o raise",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}}})`, "mongodb+wrong://invalid-uri"),
			"",
			`eval_builtin_error: mongodb.send: error parsing uri: scheme must be "mongodb" or "mongodb+srv"`,
			false,
			now,
			0,
		},
		{
			"error with raise",
			fmt.Sprintf(`p := mongodb.send({"uri": "%s", "find": {"database": "database", "collection": "collection", "filter": {"foo": "x"}}, "raise_error": false})`, "mongodb+wrong://invalid-uri"),
			`{{"result": {"p": {"error": {"message": "error parsing uri: scheme must be \"mongodb\" or \"mongodb+srv\""}}}}}`,
			"",
			false,
			now,
			0,
		},
	}

	var maxSize int64 = 1024 * 1024
	interQueryCache := cache.NewInterQueryCache(&cache.Config{
		InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
			MaxSizeBytes: &maxSize,
		},
	})

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			if !tc.doNotResetCache {
				interQueryCache = cache.NewInterQueryCache(&cache.Config{
					InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
						MaxSizeBytes: &maxSize,
					},
				})
			}

			executeMongoDB(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits)
		})
	}
}

func executeMongoDB(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModule(module),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}

	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())
	metrics := metrics.New()
	v, err := vm.NewVM().WithExecutable(executable).Eval(ctx, query, vm.EvalOpts{
		Metrics:                metrics,
		Time:                   time,
		Cache:                  builtins.Cache{},
		InterQueryBuiltinCache: interQueryCache,
		StrictBuiltinErrors:    true,
	})
	if expectedError != "" {
		if !strings.HasPrefix(err.Error(), expectedError) {
			tb.Fatalf("unexpected error: %v", err)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}

	if hits := metrics.Counter(mongoDBSendInterQueryCacheHits).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}

func startMongoDB(t *testing.T, username string, password string) (testcontainers.Container, string) {
	t.Helper()

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			ConfigModifier: func(config *container.Config) {
				config.Env = []string{
					fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s", username),
					fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", password),
				}
			},
			Image:        "mongo:6",
			ExposedPorts: []string{"27017/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForLog("Waiting for connections"),
				wait.ForListeningPort("27017/tcp"),
			),
		},
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

	collection := client.Database("database").Collection("collection")

	if _, err := collection.InsertOne(ctx, bson.D{{Key: "foo", Value: "x"}, {Key: "bar", Value: 1}}); err != nil {
		t.Fatal(err)
	}

	return container, endpoint
}

func authMongoURI(uri string, username string, password string) string {
	return strings.ReplaceAll(uri, "localhost", username+":"+password+"@localhost")
}
