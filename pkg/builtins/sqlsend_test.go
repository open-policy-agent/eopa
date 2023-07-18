package builtins

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

type backend struct {
	conn    string
	cleanup func()
}

func TestSQLSend(t *testing.T) {
	type typ int
	const (
		sqlite typ = iota
		mysql
		postgres
		none
	)

	setupSQLite := func(t *testing.T) backend {
		file := t.TempDir() + "/sqlite3.db"
		populate(t, file)
		return backend{conn: file, cleanup: func() {}}
	}

	backends := map[typ]backend{
		sqlite:   setupSQLite(t),
		mysql:    startMySQL(t),
		postgres: startPostgreSQL(t),
	}

	now := time.Now()

	tests := []struct {
		note                string
		backend             typ
		query               string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
		preparedQueries     int
	}{
		{
			note:    "missing parameter(s)",
			backend: none,
			query:   `p = resp { sql.send({}, resp)}`,
			error:   `eval_type_error: sql.send: operand 1 missing required request parameters(s): {"data_source_name", "driver", "query"}`,
		},
		{
			note:            "a single row query",
			backend:         sqlite,
			query:           `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			preparedQueries: 1,
		},
		{
			note:            "a multi-row query",
			backend:         sqlite,
			query:           `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T2"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["A1", "B1"], ["A2", "B2"]]}}}}`,
			preparedQueries: 1,
		},
		{
			note:            "query with args",
			backend:         sqlite,
			query:           `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1 WHERE ID = $1", "args": ["A"]}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["B"]]}}}}`,
			preparedQueries: 1,
		},
		{
			note:    "intra-query query cache",
			backend: sqlite,
			query: `p = [ resp1, resp2 ] {
sql.send({"driver": "sqlite", "data_source_name": "%[1]s", "query": "SELECT VALUE FROM T1"}, resp1)
sql.send({"driver": "sqlite", "data_source_name": "%[1]s", "query": "SELECT VALUE FROM T1"}, resp2) # cached
}`,
			result:          `{{"result": {"p": [{"rows": [["B"]]},{"rows": [["B"]]}]}}}`,
			preparedQueries: 1, // prepared query is cached so only one prepared query required
		},
		{
			note:    "inter-query query cache warmup (default duration)",
			backend: sqlite,
			query:   `p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:  `{{"result": {"p": {"rows": [["B"]]}}}}`,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			backend:             sqlite,
			query:               `p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:              `{{"result": {"p": {"rows": [["B"]]}}}}`,
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
		},
		{
			note:            "inter-query query cache check (default duration, expired)",
			backend:         sqlite,
			query:           `p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["B"]]}}}}`,
			doNotResetCache: true, // keep the warmup results
			time:            now.Add(interQueryCacheDurationDefault),
		},
		{
			note:    "inter-query query cache warmup (explicit duration)",
			backend: sqlite,
			query:   `p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:  `{{"result": {"p": {"rows": [["B"]]}}}}`,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			backend:             sqlite,
			query:               `p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:              `{{"result": {"p": {"rows": [["B"]]}}}}`,
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
		},
		{
			note:            "inter-query query cache check (explicit duration, expired)",
			backend:         sqlite,
			query:           `p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["B"]]}}}}`,
			doNotResetCache: true, // keep the warmup results
			time:            now.Add(10 * time.Second),
		},
		{
			note:            "rows as objects",
			backend:         sqlite,
			query:           `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T1", "row_object": true}, resp)}`,
			result:          `{{"result": {"p": {"rows": [{"ID": "A", "VALUE": "B"}]}}}}`,
			preparedQueries: 0, // single row query already prepared the query
		},
		{
			note:    "error w/o raise",
			backend: sqlite,
			query:   `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM NON_EXISTING"}, resp)}`,
			error:   "eval_builtin_error: sql.send: SQL logic error: no such table: NON_EXISTING (1)",
		},
		{
			note:    "error with raise",
			backend: sqlite,
			query:   `p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM NON_EXISTING", "raise_error": false}, resp)}`,
			result:  `{{"result": {"p": {"rows": [], "error": {"code": 1, "message": "SQL logic error: no such table: NON_EXISTING (1)"}}}}}`,
		},
		{
			note:            "mysql: a single row query",
			backend:         mysql,
			query:           `p = resp { sql.send({"driver": "mysql", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			preparedQueries: 1,
		},
		{
			note:            "postgresql: a single row query",
			backend:         postgres,
			query:           `p = resp { sql.send({"driver": "postgres", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			preparedQueries: 1,
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
			query := tc.query
			if tc.backend != none {
				be, ok := backends[tc.backend]
				if !ok {
					t.Skip("test skipped")
				}
				query = fmt.Sprintf(tc.query, be.conn)
			}
			if tc.time.IsZero() {
				tc.time = now
				t.Log("replaced time")
			}
			if !tc.doNotResetCache {
				interQueryCache = cache.NewInterQueryCache(&cache.Config{
					InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
						MaxSizeBytes: &maxSize,
					},
				})
			}

			execute(t, interQueryCache, "package t\n"+query, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits, tc.preparedQueries)
		})
	}
}

func execute(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, now time.Time, expectedInterQueryCacheHits int, expectedPreparedQueries int) {
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
		Time:                   now,
		Cache:                  builtins.Cache{},
		InterQueryBuiltinCache: interQueryCache,
		StrictBuiltinErrors:    true,
	})
	if expectedError != "" {
		if expectedError != err.Error() {
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

	if hits := metrics.Counter(sqlSendInterQueryCacheHits).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("inter-query cache: got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}

	if prepared := metrics.Counter(sqlSendPreparedQueries).Value().(uint64); prepared != uint64(expectedPreparedQueries) {
		tb.Fatalf("prepared queries: got %v prepared queries, wanted %v\n", prepared, expectedPreparedQueries)
	}
}

var initSQL = `
        CREATE TABLE T1 (ID TEXT, VALUE TEXT);
        CREATE TABLE T2 (ID TEXT, VALUE TEXT);

        INSERT INTO T1(ID, VALUE) VALUES('A', 'B');
        INSERT INTO T2(ID, VALUE) VALUES('A1', 'B1');
        INSERT INTO T2(ID, VALUE) VALUES('A2', 'B2');
	`

func populate(tb testing.TB, file string) {
	db, err := sql.Open("sqlite", file)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()

	if _, err = db.Exec(initSQL); err != nil {
		tb.Fatal(err)
	}
}

func startMySQL(t *testing.T) backend {
	t.Helper()

	srv, err := mysql.RunContainer(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	connStr, err := srv.ConnectionString(context.Background(), "tls=skip-verify")
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range strings.Split(initSQL, ";") {
		if s := strings.TrimSpace(s); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatal(err)
			}
		}
	}

	return backend{conn: connStr, cleanup: func() { srv.Terminate(context.Background()) }}
}

func startPostgreSQL(t *testing.T) backend {
	t.Helper()

	srv, err := postgres.RunContainer(context.Background(),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatal(err)
	}

	connStr, err := srv.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range strings.Split(initSQL, ";") {
		if s := strings.TrimSpace(s); s != "" {
			if _, err := db.Exec(s); err != nil {
				t.Fatal(err)
			}
		}
	}

	return backend{conn: connStr, cleanup: func() { srv.Terminate(context.Background()) }}
}
