package builtins

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/library"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func TestPostgresEnvSend(t *testing.T) {
	t.Parallel()

	be := startPostgreSQL(t)
	t.Cleanup(be.cleanup)

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	// first we dissect the conn string into user, password, host etc
	// and set them as temporary env vars
	u, err := url.Parse(be.conn)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.User.Password()
	env := map[string]string{
		"PGHOST":     u.Hostname(),
		"PGPORT":     u.Port(),
		"PGUSER":     u.User.Username(),
		"PGPASSWORD": p,
		"PGDBNAME":   u.Path,
		"PGSSLMODE":  u.Query().Get("sslmode"),
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "send",
			query: "x := data.example.p",
			module: `
p := count(postgres.send("SELECT 1 FROM T1 WHERE $1", [input.x]).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send with options",
			query: "x := data.example.p",
			module: `
p := count(postgres.send_opts("SELECT 1 FROM T1 WHERE $1", [input.x], {"cache": true, "cache_duration": "10s", "raise_error": false}).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(tc.exp,
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.postgres.v1.env as postgres
`+tc.module),
			rego.Query(tc.query),
			rego.Input(map[string]any{"x": true}),
		))
	}
}

func TestPostgresVaultSend(t *testing.T) {
	t.Parallel()

	be := startPostgreSQL(t)
	t.Cleanup(be.cleanup)

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	// first we dissect the conn string into user, password, host etc
	// and set them as vault data
	u, err := url.Parse(be.conn)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.User.Password()
	databag := map[string]string{
		"host":     u.Hostname(),
		"port":     u.Port(),
		"user":     u.User.Username(),
		"password": p,
		"dbname":   u.Path,
		"sslmode":  u.Query().Get("sslmode"),
	}

	secrets := map[string]map[string]string{
		"postgres":         databag,
		"overridepostgres": databag,
	}

	tc, addr, token := startVaultMulti(t, "secret", secrets)
	t.Cleanup(func() { tc.Terminate(context.Background()) })

	env := map[string]string{
		"VAULT_ADDRESS":     addr,
		"VAULT_TOKEN":       token,
		"OTHER_ENV_ADDRESS": addr,
		"OTHER_ENV_TOKEN":   token,
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "send",
			query: "x := data.example.p",
			module: `
p := count(postgres.send("SELECT 1 FROM T1 WHERE $1", [input.x]).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send with vault overrides",
			query: "x := data.example.p",
			module: `
import data.system.eopa.utils.vault.v1.env as vault
postgres_send(query, args) := result {
	result := postgres.send(query, args) with postgres.override.secret_path as "secret/overridepostgres"
		with vault.override.address as opa.runtime().env.OTHER_ENV_ADDRESS
		with vault.override.token as opa.runtime().env.OTHER_ENV_TOKEN
}
p := count(postgres_send("SELECT 1 FROM T1 WHERE $1", [input.x]).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send with options",
			query: "x := data.example.p",
			module: `
p := count(postgres.send_opts("SELECT 1 FROM T1 WHERE $1", [input.x], {"cache": true, "cache_duration": "10s", "raise_error": false}).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(tc.exp,
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.postgres.v1.vault as postgres
`+tc.module),
			rego.Query(tc.query),
			rego.Input(map[string]any{"x": true}),
		))
	}
}

func TestMysqlVaultSend(t *testing.T) {
	t.Parallel()

	be := startMySQL(t)
	t.Cleanup(be.cleanup)

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`^([a-z]+):([a-z]+)@tcp\(([a-z0-9:.]+)\)/([a-z]+)\?tls=([a-z-]+)$`)
	ms := re.FindStringSubmatch(be.conn)
	if len(ms) < 6 {
		t.Fatalf("failed to parse conn string %s => %v", be.conn, ms)
	}
	host, port, _ := net.SplitHostPort(ms[3])

	databag := map[string]string{
		"user":     ms[1],
		"password": ms[2],
		"host":     host,
		"port":     port,
		"dbname":   ms[4],
		"tls":      ms[5],
	}

	secrets := map[string]map[string]string{
		"mysql":         databag,
		"overridemysql": databag,
	}

	tc, addr, token := startVaultMulti(t, "secret", secrets)
	t.Cleanup(func() { tc.Terminate(context.Background()) })

	env := map[string]string{
		"VAULT_ADDRESS":     addr,
		"VAULT_TOKEN":       token,
		"OTHER_ENV_ADDRESS": addr,
		"OTHER_ENV_TOKEN":   token,
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "send",
			query: "x := data.example.p",
			module: `
p := count(mysql.send("SELECT 1 FROM T1 WHERE ?", [input.x]).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send with vault overrides",
			query: "x := data.example.p",
			module: `
import data.system.eopa.utils.vault.v1.env as vault
mysql_send(query, args) := result {
	result := mysql.send(query, args) with mysql.override.secret_path as "secret/overridemysql"
		with vault.override.address as opa.runtime().env.OTHER_ENV_ADDRESS
		with vault.override.token as opa.runtime().env.OTHER_ENV_TOKEN
}
p := count(mysql_send("SELECT 1 FROM T1 WHERE ?", [input.x]).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send with options",
			query: "x := data.example.p",
			module: `
p := count(mysql.send_opts("SELECT 1 FROM T1 WHERE ?", [input.x], {"cache": true, "cache_duration": "10s", "raise_error": false}).rows)
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := json.Number("1")
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(tc.exp,
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.mysql.v1.vault as mysql
`+tc.module),
			rego.Query(tc.query),
			rego.Input(map[string]any{"x": true}),
		))
	}
}

func TestVaultSecret(t *testing.T) {
	t.Parallel()

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	tc, addr, token := startVault(t, "a", "b/c/d", map[string]string{"fox": "trot"})
	t.Cleanup(func() { tc.Terminate(context.Background()) })

	env := map[string]string{
		"VAULT_ADDRESS":     addr,
		"VAULT_TOKEN":       token,
		"OTHER_ENV_ADDRESS": addr,
		"OTHER_ENV_TOKEN":   token,
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "secret",
			query: "x := data.example.p",
			module: `
p := vault.secret("a/b/c/d")
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := map[string]any{"fox": "trot"}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "secret with overrides",
			query: "x := data.example.p",
			module: `
vault_secret(path) := result {
	result := vault.secret(path)
		with vault.override.address as opa.runtime().env.OTHER_ENV_ADDRESS
		with vault.override.token as opa.runtime().env.OTHER_ENV_TOKEN
}
p := vault_secret("a/b/c/d")
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := map[string]any{"fox": "trot"}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "secret_opts",
			query: "x := data.example.p",
			module: `
p := vault.secret_opts("a/b/c/d", {"cache": true, "cache_duration": "10s", "raise_error": false})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"]
				exp := map[string]any{"fox": "trot"}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(tc.exp,
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.vault.v1.env as vault
`+tc.module),
			rego.Query(tc.query),
		))
	}
}

func TestMongoDBFindVault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	username, password := "alice", "wasspord"
	tc, endpoint := startMongoDB(t, username, password)
	t.Cleanup(func() { tc.Terminate(ctx) })

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	databag := map[string]string{
		"host":     u.Hostname(),
		"port":     u.Port(),
		"user":     username,
		"password": password,
	}
	secrets := map[string]map[string]string{
		"mongodb":         databag,
		"overridemongodb": databag,
	}

	tc, addr, token := startVaultMulti(t, "secret", secrets)
	t.Cleanup(func() { tc.Terminate(context.Background()) })

	nonEmpty := func(f func(*testing.T, rego.ResultSet)) func(*testing.T, rego.ResultSet) {
		return func(t *testing.T, rs rego.ResultSet) {
			if len(rs) == 0 {
				t.Fatal("expected non-empty result")
			}
			f(t, rs)
		}
	}

	env := map[string]string{
		"VAULT_ADDRESS": addr,
		"VAULT_TOKEN":   token,
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "find with filter",
			query: "x := data.example.p.results[0]",
			module: `
p := mongodb.find({"database": "database", "collection": "collection", "filter": {"foo": input.x}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				delete(act, "_id")
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find with filter and overrides",
			query: "x := data.example.p.results[0]",
			module: `
mongodb_find(req) := result {
	result := mongodb.find(req)
		with mongodb.override.secret_path as "secret/overridemongodb"
}
p := mongodb_find({"database": "database", "collection": "collection", "filter": {"foo": input.x}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				delete(act, "_id")
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find with options",
			query: "x := data.example.p.results[0]",
			module: `
p := mongodb.find({"database": "database", "collection": "collection", "filter": {}, "options": {"projection": {"_id": false}}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find with cache+error options",
			query: "x := data.example.p.results[0]",
			module: `
p := mongodb.find({"database": "database", "collection": "collection", "filter": {}, "options": {"projection": {"_id": false}}, "cache": true, "cache_duration": "10s", "raise_error": false})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find_one with filter",
			query: "x := data.example.p.results",
			module: `
p := mongodb.find_one({"database": "database", "collection": "collection", "filter": {"foo": input.x}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				delete(act, "_id")
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find_one with options",
			query: "x := data.example.p.results",
			module: `
p := mongodb.find_one({"database": "database", "collection": "collection", "filter": {}, "options": {"projection": {"_id": false}}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "find_one with cache+error options",
			query: "x := data.example.p.results",
			module: `
p := mongodb.find_one({"database": "database", "collection": "collection", "filter": {}, "options": {"projection": {"_id": false}}, "cache": true, "cache_duration": "10s", "raise_error": false})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"bar": json.Number("1"),
					"foo": "x",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(nonEmpty(tc.exp),
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.mongodb.v1.vault as mongodb
`+tc.module),
			rego.Query(tc.query),
			rego.Input(map[string]any{"x": "x"}),
		))
	}
}

func TestDynamoDBGetVault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tc, endpoint := startDynamoDB(t)
	t.Cleanup(func() { tc.Terminate(ctx) })

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	dummy := "dummy"
	databag := map[string]string{
		"endpoint":   endpoint,
		"region":     "us-west-2",
		"access_key": dummy,
		"secret_key": dummy,
	}

	secrets := map[string]map[string]string{
		"dynamodb":         databag,
		"overridedynamodb": databag,
	}

	tc, addr, token := startVaultMulti(t, "secret", secrets)
	t.Cleanup(func() { tc.Terminate(ctx) })

	env := map[string]string{
		"VAULT_ADDRESS": addr,
		"VAULT_TOKEN":   token,
	}

	tests := []struct {
		note   string
		query  string
		module string
		exp    func(*testing.T, rego.ResultSet)
	}{
		{
			note:  "send/get",
			query: "x := data.example.p.row",
			module: `
p := dynamodb.get({"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"number": json.Number("1234"),
					"p":      "x",
					"s":      json.Number("1"),
					"string": "value",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send/get with override",
			query: "x := data.example.p.row",
			module: `
dynamodb_get(req) := result {
	result := dynamodb.get(req)
		with dynamodb.override.secret_path as "secret/overridedynamodb"
}
p := dynamodb_get({"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].(map[string]any)
				exp := map[string]any{
					"number": json.Number("1234"),
					"p":      "x",
					"s":      json.Number("1"),
					"string": "value",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
		{
			note:  "send/query",
			query: "x := data.example.p.rows",
			module: `
p := dynamodb.query({"table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})
`,
			exp: func(t *testing.T, rs rego.ResultSet) {
				act := rs[0].Bindings["x"].([]any)
				exp := []any{
					map[string]any{
						"number": json.Number("1234"),
						"p":      "x",
						"s":      json.Number("1"),
						"string": "value",
					},
					map[string]any{
						"number": json.Number("4321"),
						"p":      "x",
						"s":      json.Number("2"),
						"string": "value2",
					},
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, runRegoTests(tc.exp,
			rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
			rego.Module("example.rego", `package example
import data.system.eopa.utils.dynamodb.v1.vault as dynamodb
`+tc.module),
			rego.Query(tc.query),
			rego.Input(map[string]any{"x": "x"}),
		))
	}
}

func runRegoTests(exp func(*testing.T, rego.ResultSet), r ...func(*rego.Rego)) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		ctx := context.Background()
		opts := []func(*rego.Rego){
			rego.Target(rego_vm.Target),
			rego.StrictBuiltinErrors(true),
			rego.InterQueryBuiltinCache(newInterQueryCache()),
		}
		rs, err := rego.New(append(opts, r...)...).Eval(ctx)
		if err != nil {
			t.Fatal(err)
		}

		exp(t, rs)
	}
}

func newInterQueryCache() cache.InterQueryCache {
	var maxSize int64 = 1024 * 1024
	var evictPct int64 = 100
	var staleEvictSec int64 = 0
	return cache.NewInterQueryCache(&cache.Config{
		InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
			MaxSizeBytes:                      &maxSize,
			ForcedEvictionThresholdPercentage: &evictPct,
			StaleEntryEvictionPeriodSeconds:   &staleEvictSec,
		},
	})
}
