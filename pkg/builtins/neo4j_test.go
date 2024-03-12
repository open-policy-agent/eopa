package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"

	"github.com/styrainc/enterprise-opa-private/pkg/library"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestNeo4jQuery(t *testing.T) {
	t.Parallel()

	// force the library methods to load so we can access the vault helpers
	err := library.Init()
	if err != nil {
		t.Fatal(err)
	}

	username := "neo4j"
	password := "letmein!"
	neo4jContainer, uri := startNeo4j(t, username, password)
	defer neo4jContainer.Terminate(context.Background())

	vaultData := map[string]map[string]string{
		"neo4j": {
			"uri":         uri,
			"scheme":      "basic",
			"principal":   username,
			"credentials": password,
		},
	}
	vaultContainer, vaultURI, vaultToken := startVaultMulti(t, "secret", vaultData)
	defer vaultContainer.Terminate(context.Background())

	auth := fmt.Sprintf(`{"scheme": "basic", "principal": "%s", "credentials": "%s"}`, username, password)
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
			Note:                "hello world",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = \"dog456\" RETURN n.name"})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "rintintin"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "select with multiple results",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.age > 3 RETURN n.name"})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "spot"}, {"n.name": "mittens"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note: "select with object-valued results",
			// the set comprehension deals with the fact that the
			// default return from neo4j include an ElementID,
			// which has a non-deterministically assigned UUID as a
			// value.
			Source:              fmt.Sprintf(`p := {r.n.Props | r := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.age > 3 RETURN n"}).results[_]}`, auth, uri),
			Result:              `{{"result": {"p": {{"adopted": true, "age": 5, "breed": "tabby", "id": "cat456", "name": "mittens"}, {"adopted": true, "age": 7, "breed": "beagle", "id": "dog790", "name": "spot"}}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "select with string parameter",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = $id RETURN n.name", "parameters": {"id": "dog789"}})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "lassie"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "select over relationships with multiple results",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (o: Person)-[:OWNS]->(p:Pet) WHERE o.name = $on RETURN p.name AS name ORDER BY p.name", "parameters": {"on": "eve"}})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"name": "mittens"}, {"name": "norbert"}, {"name": "wilhelm"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "select with multiple results and numeric parameter",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.age > $a RETURN n.name", "parameters": {"a": 3}})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "spot"}, {"n.name": "mittens"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "missing parameter should error",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.age > $a RETURN n.name", "parameters": {"x": 3}})`, auth, uri),
			Error:               "eval_builtin_error: neo4j.query: Neo4jError: Neo.ClientError.Statement.ParameterMissing (Expected parameter(s): a)",
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "raise_error=false should cause an error result",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.age > $a RETURN n.name", "parameters": {"x": 3}, "raise_error": false})`, auth, uri),
			Result:              `{{"result": {"p": {"error": "Neo4jError: Neo.ClientError.Statement.ParameterMissing (Expected parameter(s): a)"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "missing required key should error",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s"})`, auth, uri),
			Error:               `eval_type_error: neo4j.query: operand 1 missing required request parameter(s): {"query"}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "extra key should error",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "foo", "extra": "bar"})`, auth, uri),
			Error:               `eval_type_error: neo4j.query: operand 1 invalid request parameter(s): {"extra"}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "test cache part (warm cache)",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = \"dog456\" RETURN n.name", "cache": true})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "rintintin"}]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "test cache part (request cached data)",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = \"dog456\" RETURN n.name", "cache": true})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "rintintin"}]}}}}`,
			DoNotResetCache:     true,
			Time:                now,
			InterQueryCacheHits: 1,
		},
		{
			// This makes sure we're using a cache key that has the
			// query as part of it, lest we should risk polluting
			// data between cache accesses.
			Note:                "test cache part (request non-cached data)",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = \"dog789\" RETURN n.name", "cache": true})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "lassie"}]}}}}`,
			DoNotResetCache:     true,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "test cache part (request cache data after expiry)",
			Source:              fmt.Sprintf(`p := neo4j.query({"auth": %s, "uri": "%s", "query": "MATCH (n:Pet) WHERE n.id = \"dog456\" RETURN n.name", "cache": true})`, auth, uri),
			Result:              `{{"result": {"p": {"results": [{"n.name": "rintintin"}]}}}}`,
			DoNotResetCache:     true,
			Time:                now.Add(time.Hour * 1000),
			InterQueryCacheHits: 0,
		},

		// this is mostly a sanity check that we loaded the helpers and
		// populated data into the vault sever correctly, we just check
		// the auth field since the URI is non-deterministic due to
		// containertest
		{
			Note: "test vault helper auth()",
			Source: fmt.Sprintf(`import data.system.eopa.utils.neo4j.v1.vault as n4jvault
import data.system.eopa.utils.vault.v1.env as vault
out = y {
	x := n4jvault.auth(vault.secret("secret/neo4j")) with vault.override.address as "%s" with vault.override.token as "%s"
	y := x.auth
}
`, vaultURI, vaultToken),
			Result: `{{"result": {"out": {"credentials": "letmein!", "principal": "neo4j", "realm": "", "scheme": "basic"}}}}`,
		},

		// test that we can successfully use query() via the helper,
		// using a previous test case
		{
			Note: "test vault helper query()",
			Source: fmt.Sprintf(`import data.system.eopa.utils.neo4j.v1.vault as n4jvault
import data.system.eopa.utils.vault.v1.env as vault
out = y {
	y := n4jvault.query({"query": "MATCH (n:Pet) WHERE n.age > 3 RETURN n.name"}) with vault.override.address as "%s" with vault.override.token as "%s"
}
`, vaultURI, vaultToken),
			Result: `{{"result": {"out": {"results": [{"n.name": "spot"}, {"n.name": "mittens"}]}}}}`,
		},
	}

	interQueryCache := newInterQueryCache()

	for _, tc := range tests {
		t.Run(tc.Note, func(t *testing.T) {
			if !tc.DoNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			executeNeo4j(t, interQueryCache, "package t\n"+tc.Source, "t", tc.Result, tc.Error, tc.Time, tc.InterQueryCacheHits)
		})
	}
}

func executeNeo4j(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int) {
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

	hits := metrics.Counter(neo4jQueryInterQueryCacheHitsKey).Value().(uint64)
	if hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}

func startNeo4j(t *testing.T, username, password string) (testcontainers.Container, string) {
	t.Helper()

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "neo4j:5.13.0-bullseye",
			ExposedPorts: []string{"7687/tcp", "7474/tcp"},
			Env: map[string]string{
				"NEO4J_AUTH": username + "/" + password,
			},

			WaitingFor: wait.ForAll(
				wait.ForLog("Started."),
				wait.ForListeningPort("7474/tcp"),
				wait.ForListeningPort("7687/tcp"),
			),
		},
		Logger:  testcontainers.TestLogger(t),
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := container.PortEndpoint(ctx, "7687/tcp", "neo4j")
	if err != nil {
		t.Fatal(err)
	}

	driver, err := neo4j.NewDriverWithContext(endpoint, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(context.Background())

	pets := []map[string]any{
		{
			"id":      "dog123",
			"adopted": true,
			"age":     2,
			"breed":   "terrier",
			"name":    "toto",
		},
		{
			"id":      "dog456",
			"adopted": false,
			"age":     3,
			"breed":   "german-shepherd",
			"name":    "rintintin",
		},
		{
			"id":      "dog789",
			"adopted": false,
			"age":     2,
			"breed":   "collie",
			"name":    "lassie",
		},
		{
			"id":      "dog790",
			"adopted": true,
			"age":     7,
			"breed":   "beagle",
			"name":    "spot",
		},
		{
			"id":      "cat123",
			"adopted": false,
			"age":     1,
			"breed":   "fictitious",
			"name":    "cheshire",
		},
		{
			"id":      "cat456",
			"adopted": true,
			"age":     5,
			"breed":   "tabby",
			"name":    "mittens",
		},
		{
			"id":      "cat789",
			"adopted": false,
			"age":     2,
			"breed":   "calico",
			"name":    "fred",
		},
		{
			"id":      "cat790",
			"adopted": true,
			"age":     1,
			"breed":   "calico",
			"name":    "norbert",
		},
		{
			"id":      "cat791",
			"adopted": true,
			"age":     2,
			"breed":   "sphinx",
			"name":    "wilhelm",
		},
	}

	for _, pet := range pets {
		_, err = neo4j.ExecuteQuery(
			ctx,
			driver,
			"CREATE (n:Pet { id: $id, adopted: $adopted, age: $age, breed: $breed, name: $name }) RETURN n",
			pet,
			neo4j.EagerResultTransformer,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	people := []map[string]any{
		{
			"id":     "person123",
			"name":   "alice",
			"tenure": 20,
			"title":  "owner",
		},
		{
			"id":     "person456",
			"name":   "bob",
			"tenure": 15,
			"title":  "employee",
		},
		{
			"id":     "person789",
			"name":   "eve",
			"tenure": 5,
			"title":  "employee",
		},
		{
			"id":     "person790",
			"name":   "dave",
			"tenure": 3,
			"title":  "customer",
		},
		{
			"id":     "person791",
			"name":   "mike",
			"tenure": 4,
			"title":  "customer",
		},
	}

	for _, person := range people {
		_, err = neo4j.ExecuteQuery(
			ctx,
			driver,
			"CREATE (n:Person { id: $id, name: $name, tenure: $tenure, title: $title}) RETURN n",
			person,
			neo4j.EagerResultTransformer,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	owners := []map[string]any{
		{
			"owner": "person791",
			"pet":   "cat123",
		},
		{
			"owner": "person790",
			"pet":   "dog790",
		},
		{
			"owner": "person789",
			"pet":   "cat456",
		},
		{
			"owner": "person789",
			"pet":   "cat790",
		},
		{
			"owner": "person789",
			"pet":   "cat791",
		},
	}

	for _, owner := range owners {
		_, err = neo4j.ExecuteQuery(
			ctx,
			driver,
			"MATCH (o:Person)\nMATCH(p:Pet)\nWHERE p.id=$pet AND o.id=$owner\nMERGE (o)-[:OWNS]->(p)",
			owner,
			neo4j.EagerResultTransformer,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return container, endpoint
}
