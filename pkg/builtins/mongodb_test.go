package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"

	"github.com/testcontainers/testcontainers-go"
	tc_log "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/open-policy-agent/eopa/pkg/vm"
)

func TestMongoDBFind(t *testing.T) {
	t.Parallel()

	username, password := "root", "password"
	mongodb, uri := startMongoDB(t, username, password)
	defer mongodb.Terminate(context.Background())

	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)
	now := time.Now()

	tests := []struct {
		Note                string
		Source              string
		Result              string
		Error               string
		DoNotResetCache     bool
		Time                time.Time
		InterQueryCacheHits int
	}{
		{
			Note:                "missing parameter(s)",
			Source:              `p := mongodb.find({})`,
			Result:              "",
			Error:               `eval_type_error: mongodb.find: operand 1 missing required request parameter(s): {"uri"}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "non-existing doc query (find many)",
			Source:              fmt.Sprintf(`p := mongodb.find({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "nonexisting"}})`, uri, auth),
			Result:              `{{"result": {"p": {}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "a single row query (find many)",
			Source:              fmt.Sprintf(`p := mongodb.find({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "a single row query (find many, canonical)",
			Source:              fmt.Sprintf(`p := mongodb.find({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}, "canonical": true})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": {"$numberInt": "1"}, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note: "intra-query query cache",
			Source: fmt.Sprintf(`p = [ resp1, resp2 ] if {
                                mongodb.find({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}}, resp1)
                                mongodb.find({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}}, resp2) # cached
				}`, uri, auth, uri, auth),
			Result:              `{{"result": {"p": [{"results": [{"bar": 1, "foo": "x"}]}, {"results": [{"bar": 1, "foo": "x"}]}]}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache warmup (default duration)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "uri": "%s", "auth" :%s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache check (default duration, valid)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(interQueryCacheDurationDefault - 1),
			InterQueryCacheHits: 1,
		},
		{
			Note:                "inter-query query cache check (default duration, expired)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(interQueryCacheDurationDefault),
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache warmup (explicit duration)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache check (explicit duration, valid)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(10*time.Second - 1),
			InterQueryCacheHits: 1,
		},
		{
			Note:                "inter-query query cache check (explicit duration, expired)",
			Source:              fmt.Sprintf(`p := mongodb.find({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": [{"bar": 1, "foo": "x"}]}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(10 * time.Second),
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error w/o raise",
			Source:              fmt.Sprintf(`p := mongodb.find({"uri": "%s", "database": "database", "collection": "collection", "filter": {"foo": "x"}})`, "mongodb+wrong://invalid-uri"),
			Result:              "",
			Error:               `eval_builtin_error: mongodb.find: error parsing uri: scheme must be "mongodb" or "mongodb+srv"`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error with raise",
			Source:              fmt.Sprintf(`p := mongodb.find({"uri": "%s", "database": "database", "collection": "collection", "filter": {"foo": "x"}, "raise_error": false})`, "mongodb+wrong://invalid-uri"),
			Result:              `{{"result": {"p": {"error": {"message": "error parsing uri: scheme must be \"mongodb\" or \"mongodb+srv\""}}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			if !tc.DoNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeMongoDB(t, interQueryCache, "package t\n"+tc.Source, "t", tc.Result, tc.Error, tc.Time, tc.InterQueryCacheHits)
		})
	}
}

func TestMongoDBFindOne(t *testing.T) {
	t.Parallel()

	username, password := "root", "password"
	mongodb, uri := startMongoDB(t, username, password)
	defer mongodb.Terminate(context.Background())

	auth := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, username, password)
	now := time.Now()

	tests := []struct {
		Note                string
		Source              string
		Result              string
		Error               string
		DoNotResetCache     bool
		Time                time.Time
		InterQueryCacheHits int
	}{
		{
			Note:                "missing parameter(s)",
			Source:              `p := mongodb.find_one({})`,
			Result:              "",
			Error:               `eval_type_error: mongodb.find_one: operand 1 missing required request parameter(s): {"uri"}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "non-existing doc query (find one)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "nonexisting"}})`, uri, auth),
			Result:              `{{"result": {"p": {}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "a single row query (find one)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}, "show_record_id": true}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x", "$recordId": 1}}}}}`, // recordId is included only if show_record_id is converted to camel casing correctly.
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "a single row query (find one, canonical)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}, "canonical": true})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": {"$numberInt": "1"}, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note: "intra-query query cache",
			Source: fmt.Sprintf(`p = [ resp1, resp2 ] if {
                                mongodb.find_one({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}}, resp1)
                                mongodb.find_one({"uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}}, resp2) # cached
				}`, uri, auth, uri, auth),
			Result:              `{{"result": {"p": [{"results": {"bar": 1, "foo": "x"}}, {"results": {"bar": 1, "foo": "x"}}]}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache warmup (default duration)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "uri": "%s", "auth" :%s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache check (default duration, valid)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(interQueryCacheDurationDefault - 1),
			InterQueryCacheHits: 1,
		},
		{
			Note:                "inter-query query cache check (default duration, expired)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(interQueryCacheDurationDefault),
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache warmup (explicit duration)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "inter-query query cache check (explicit duration, valid)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(10*time.Second - 1),
			InterQueryCacheHits: 1,
		},
		{
			Note:                "inter-query query cache check (explicit duration, expired)",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"cache": true, "cache_duration": "10s", "uri": "%s", "auth": %s, "database": "database", "collection": "collection", "filter": {"foo": "x"}, "options": {"projection": {"_id": false}}})`, uri, auth),
			Result:              `{{"result": {"p": {"results": {"bar": 1, "foo": "x"}}}}}`,
			Error:               "",
			DoNotResetCache:     true, // keep the warmup results
			Time:                now.Add(10 * time.Second),
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error w/o raise",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"uri": "%s", "database": "database", "collection": "collection", "filter": {"foo": "x"}})`, "mongodb+wrong://invalid-uri"),
			Result:              "",
			Error:               `eval_builtin_error: mongodb.find_one: error parsing uri: scheme must be "mongodb" or "mongodb+srv"`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error with raise",
			Source:              fmt.Sprintf(`p := mongodb.find_one({"uri": "%s", "database": "database", "collection": "collection", "filter": {"foo": "x"}, "raise_error": false})`, "mongodb+wrong://invalid-uri"),
			Result:              `{{"result": {"p": {"error": {"message": "error parsing uri: scheme must be \"mongodb\" or \"mongodb+srv\""}}}}}`,
			Error:               "",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			if !tc.DoNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeMongoDB(t, interQueryCache, "package t\n"+tc.Source, "t", tc.Result, tc.Error, tc.Time, tc.InterQueryCacheHits)
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

	// HACK(philip): This hack of adding the counters together allows us to
	// reuse this function for testing both mongodb.find and find_one
	// operations, although it will require some care if we ever mix the
	// two operations in a unit test.
	hitsFind := metrics.Counter(mongoDBFindInterQueryCacheHits).Value().(uint64)
	hitsFindOne := metrics.Counter(mongoDBFindOneInterQueryCacheHits).Value().(uint64)
	if hits := hitsFind + hitsFindOne; hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
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
