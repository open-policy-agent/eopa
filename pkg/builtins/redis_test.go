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
	"github.com/open-policy-agent/eopa/pkg/vm"

	"github.com/redis/go-redis/v9"

	"github.com/testcontainers/testcontainers-go"
	tc_log "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisQuery(t *testing.T) {
	t.Parallel()

	password := "letmein!"
	username := "redis"
	redisContainer, addr := startRedis(t, username, password)
	defer redisContainer.Terminate(context.Background())

	vaultData := map[string]map[string]string{
		"redis": {
			"password": password,
			"username": username,
		},
	}
	vaultContainer, vaultURI, vaultToken := startVaultMulti(t, "secret", vaultData)
	defer vaultContainer.Terminate(context.Background())

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
		// NOTE: HKEYS is not tested because the key ordering is not
		// constant each time. SRANDMEMBER and HRANDFIELD are not
		// tested because they, by design, return a random element.

		{
			Note:                "hello world",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": ["foo"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "Hello, World!"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "atomic get",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "MGET", "args": ["foo", "bar", "baz"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["Hello, World!", "abcd", "efgh"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "get range (substring)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GETRANGE", "args": ["foo", 1, 3]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "ell"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "get string length",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "STRLEN", "args": ["foo"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 13}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "access entire list",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "LRANGE", "args": ["mylist", 0, -1]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["lamb", "ham", "spam"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "access part of a list",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "LRANGE", "args": ["mylist", 1, 1]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["ham"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "locate item in list",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "LPOS", "args": ["mylist", "spam"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 2}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "access index of a list",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "LINDEX", "args": ["mylist", 1]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "ham"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "get length of list",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "LLEN", "args": ["mylist"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 3}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "access hash table key",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HGET", "args": ["myhash", "abc"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "123"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "access entire hash table",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HGETALL", "args": ["myhash"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": {"abc": "123", "def": "789", "xyz": "456"}}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "check if item in hash table (positive)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HEXISTS", "args": ["myhash", "abc"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": true}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "check if item in hash table (negative)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HEXISTS", "args": ["myhash", "nonexistant"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": false}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "get hashtable size",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HLEN", "args": ["myhash"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 3}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "get multiple hashtable elements",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "HMGET", "args": ["myhash", "xyz", "def"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["456", "789"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set cardinality",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SCARD", "args": ["set1"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 3}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set difference 01",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SDIFF", "args": ["set1", "set2"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["aaa", "bbb"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "set difference 02",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SDIFF", "args": ["set2", "set1"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["ddd", "eee"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set intersect",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SINTER", "args": ["set1", "set2"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["ccc"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set intersect cardinality",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SINTERCARD", "args": [2, "set1", "set2"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": 1}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set membership (positive)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SISMEMBER", "args": ["set1", "aaa"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": true}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "set membership (negative)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SISMEMBER", "args": ["set1", "xxx"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": false}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "set membership (multi)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SMISMEMBER", "args": ["set1", "aaa", "xxx", "bbb"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": [true, false, true]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "access all set members",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SMEMBERS", "args": ["set1"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["aaa", "bbb", "ccc"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "set union",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "SUNION", "args": ["set1", "set2"]})`, addr, auth),
			Result:              `{{"result": {"p": {"results": ["aaa", "bbb", "ccc", "ddd", "eee"]}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note:                "test cache (warm)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": ["foo"], "cache": true})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "Hello, World!"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "test cache (hit)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": ["foo"], "cache": true})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "Hello, World!"}}}}`,
			DoNotResetCache:     true,
			Time:                now,
			InterQueryCacheHits: 1,
		},
		{
			Note:                "test cache (non-cached data)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": ["bar"], "cache": true})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "abcd"}}}}`,
			DoNotResetCache:     true,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "test cache (expired)",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": ["foo"], "cache": true})`, addr, auth),
			Result:              `{{"result": {"p": {"results": "Hello, World!"}}}}`,
			DoNotResetCache:     true,
			Time:                now.Add(time.Hour * 10000),
			InterQueryCacheHits: 0,
		},

		{
			Note: "test vault helper auth()",
			Source: fmt.Sprintf(`import data.system.eopa.utils.redis.v1.vault as redisvault
import data.system.eopa.utils.vault.v1.env as vault
out = y if {
	y := redisvault.auth(vault.secret("secret/redis")) with vault.override.address as "%s" with vault.override.token as "%s"
}
`, vaultURI, vaultToken),
			Result:              fmt.Sprintf(`{{"result": {"out": {"password": "%s", "username": "redis"}}}}`, password),
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},

		{
			Note: "test vault helper query()",
			Source: fmt.Sprintf(`import data.system.eopa.utils.redis.v1.vault as redisvault
import data.system.eopa.utils.vault.v1.env as vault
out = y if {
	y := redisvault.query({"addr": "%s", "command": "GET", "args": ["baz"]}) with vault.override.address as "%s" with vault.override.token as "%s"
}
`, addr, vaultURI, vaultToken),
			Result: `{{"result": {"out": {"results": "efgh"}}}}`,
		},

		{
			Note:                "error handling - default settings",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": []})`, addr, auth),
			Result:              `{{"result": {"p": {"error": "ERR wrong number of arguments for 'get' command"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error handling - raise_error=true",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": [], "raise_error": true})`, addr, auth),
			Result:              `{{"result": {"p": {"error": "ERR wrong number of arguments for 'get' command"}}}}`,
			DoNotResetCache:     false,
			Time:                now,
			InterQueryCacheHits: 0,
		},
		{
			Note:                "error handling - raise_error=false",
			Source:              fmt.Sprintf(`p := redis.query({"addr": "%s", "auth": %s, "command": "GET", "args": [], "raise_error": false})`, addr, auth),
			Result:              ``,
			Error:               `eval_builtin_error: redis.query: ERR wrong number of arguments for 'get' command`,
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

			executeRedis(t, interQueryCache, "package t\n"+tc.Source, "t", tc.Result, tc.Error, tc.Time, tc.InterQueryCacheHits)
		})
	}
}

func executeRedis(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int) {
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

	hits := metrics.Counter(redisQueryInterQueryCacheHitsKey).Value().(uint64)
	if hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}

func startRedis(t *testing.T, username, password string) (testcontainers.Container, string) {
	t.Helper()

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:alpine",
			ExposedPorts: []string{"6379/tcp"},

			WaitingFor: wait.ForAll(
				wait.ForLog("Ready to accept connections tcp"),
				wait.ForListeningPort("6379/tcp"),
			),
		},
		Logger:  tc_log.TestLogger(t),
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := container.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatal(err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: endpoint,
	})

	// NOTE: the password for the default user is set so that if the
	// username isn't being passed through right, the connection wont
	// authenticate. None of the tests should be using this.
	cmd := redis.NewStringCmd(ctx, "config", "set", "requirepass", "123456")
	err = rdb.Process(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// https://stackoverflow.com/a/66727309
	//
	// https://redis.io/commands/acl-setuser/
	cmd = redis.NewStringCmd(ctx, "acl", "setuser", username, "allcommands", "allkeys", "on", ">"+password)
	err = rdb.Process(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     endpoint,
		Password: password,
		Username: username,
	})

	err = rdb.Set(ctx, "foo", "Hello, World!", 0).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.Set(ctx, "bar", "abcd", 0).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.Set(ctx, "baz", "efgh", 0).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.LPush(ctx, "mylist", "spam", "ham", "lamb").Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.HSet(ctx, "myhash", map[string]any{"abc": "123", "xyz": "456", "def": "789"}).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.SAdd(ctx, "set1", []string{"aaa", "bbb", "ccc"}).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.SAdd(ctx, "set2", []string{"ccc", "ddd", "eee"}).Err()
	if err != nil {
		t.Fatal(err)
	}

	err = rdb.SAdd(ctx, "json1", "$", `{"a": "1", "b": 2, "c": {"d": 3, "e": [4,5,6]}}`).Err()
	if err != nil {
		t.Fatal(err)
	}

	return container, endpoint
}
