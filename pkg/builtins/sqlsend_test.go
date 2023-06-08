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

func TestSQLSend(t *testing.T) {
	file := t.TempDir() + "/sqlite3.db"
	populate(t, file)

	mysql, mysqlConnStr := startMySQL(t)
	defer mysql.Terminate(context.Background())

	postgres, postgresConnStr := startPostgreSQL(t)
	defer postgres.Terminate(context.Background())

	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
		preparedQueries     int
	}{
		{
			"missing parameter(s)",
			`p = resp { sql.send({}, resp)}`,
			"",
			`eval_type_error: sql.send: operand 1 missing required request parameters(s): {"data_source_name", "driver", "query"}`,
			false,
			now,
			0,
			0,
		},
		{
			"a single row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			"",
			false,
			now,
			0,
			1,
		},
		{
			"a multi-row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T2"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["A1", "B1"], ["A2", "B2"]]}}}}`,
			"",
			false,
			now,
			0,
			1,
		},
		{
			"query with args",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1 WHERE ID = $1", "args": ["A"]}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			false,
			now,
			0,
			1,
		},
		{
			"intra-query query cache",
			fmt.Sprintf(`p = [ resp1, resp2 ] {
sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp1)
sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp2) # cached
}`, file, file),
			`{{"result": {"p": [{"rows": [["B"]]},{"rows": [["B"]]}]}}}`,
			"",
			false,
			now,
			0,
			1, // prepared query is cached so only one prepared query required
		},
		{
			"inter-query query cache warmup (default duration)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			false,
			now,
			0,
			0,
		},
		{
			"inter-query query cache check (default duration, valid)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault - 1),
			1,
			0,
		},
		{
			"inter-query query cache check (default duration, expired)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault),
			0,
			0,
		},
		{
			"inter-query query cache warmup (explicit duration)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			false,
			now,
			0,
			0,
		},
		{
			"inter-query query cache check (explicit duration, valid)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(10*time.Second - 1),
			1,
			0,
		},
		{
			"inter-query query cache check (explicit duration, expired)",
			fmt.Sprintf(`p = resp { sql.send({"cache": true, "cache_duration": "10s", "driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
			true, // keep the warmup results
			now.Add(10 * time.Second),
			0,
			0,
		},
		{
			"rows as objects",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T1", "row_object": true}, resp)}`, file),
			`{{"result": {"p": {"rows": [{"ID": "A", "VALUE": "B"}]}}}}`,
			"",
			false,
			now,
			0,
			0, // single row query already prepared the query
		},
		{
			"error w/o raise",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM NON_EXISTING"}, resp)}`, file),
			"",
			"eval_builtin_error: sql.send: SQL logic error: no such table: NON_EXISTING (1)",
			false,
			now,
			0,
			0,
		},
		{
			"error with raise",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM NON_EXISTING", "raise_error": false}, resp)}`, file),
			`{{"result": {"p": {"rows": [], "error": {"code": 1, "message": "SQL logic error: no such table: NON_EXISTING (1)"}}}}}`,
			"",
			false,
			now,
			0,
			0,
		},
		{
			"mysql: a single row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "mysql", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`, mysqlConnStr),
			`{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			"",
			false,
			now,
			0,
			1,
		},
		{
			"postgresql: a single row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "postgres", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`, postgresConnStr),
			`{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			"",
			false,
			now,
			0,
			1,
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

			execute(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits, tc.preparedQueries)
		})
	}
}

func execute(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int, expectedPreparedQueries int) {
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
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}

	if prepared := metrics.Counter(sqlSendPreparedQueries).Value().(uint64); prepared != uint64(expectedPreparedQueries) {
		tb.Fatalf("got %v prepared queries, wanted %v\n", prepared, expectedPreparedQueries)
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

func startMySQL(t *testing.T) (*mysql.MySQLContainer, string) {
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

	return srv, connStr
}

func startPostgreSQL(t *testing.T) (*postgres.PostgresContainer, string) {
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

	return srv, connStr
}
