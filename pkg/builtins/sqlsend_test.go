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
	"github.com/testcontainers/testcontainers-go/modules/mssql"
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
	t.Parallel()

	type typ int
	const (
		sqlite typ = iota
		mysql
		postgres
		sqlserver
		none
	)

	setupSQLite := func(t *testing.T) backend {
		file := t.TempDir() + "/sqlite3.db"
		populate(t, file, initSQL)
		return backend{conn: file, cleanup: func() {}}
	}

	backends := map[typ]backend{
		sqlite:    setupSQLite(t),
		mysql:     startMySQL(t),
		postgres:  startPostgreSQL(t),
		sqlserver: startMSSQL(t),
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
			error:   `eval_type_error: sql.send: operand 1 missing required request parameter(s): {"data_source_name", "driver", "query"}`,
		},
		{
			note:    "unknown driver",
			backend: none,
			query:   `p = resp { sql.send({"driver": "pg", "data_source_name": "", "query": ""}, resp)}`,
			error:   `eval_type_error: sql.send: operand 1 unknown driver pg, must be one of {"mysql", "postgres", "snowflake", "sqlite"}`,
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
		{
			note:            "sqlserver: a single row query",
			backend:         sqlserver,
			query:           `p = resp { sql.send({"driver": "sqlserver", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			preparedQueries: 1,
		},
		{
			note:    "sqlserver: query with args",
			backend: sqlserver,
			// In our test schema we use text column type for convenience as it's a string type supported across all the database types we test.
			// In query processing, MS SQL Server driver converts any string query parameters automatically to varchars, which unfortunately creates
			// a problem with our schema designed to be usable across database types: in MS SQL Server, the text column type can't be compared
			// directly against varchar type. The text type is deprecated in MS SQL Server and any practical database schema would most likely
			// use varchar and no casting would be required for a simple equal test.
			query:           `p = resp { sql.send({"driver": "sqlserver", "data_source_name": "%s", "query": "SELECT VALUE FROM T1 WHERE CAST(ID AS VARCHAR) = @p1", "args": ["A"]}, resp)}`,
			result:          `{{"result": {"p": {"rows": [["B"]]}}}}`,
			preparedQueries: 1,
		},
	}

	interQueryCache := newInterQueryCache()

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
			}
			if !tc.doNotResetCache {
				interQueryCache = newInterQueryCache()
			}

			execute(t, interQueryCache, "package t\n"+query, "t", tc.result, tc.error, tc.time, nil, tc.interQueryCacheHits, tc.preparedQueries)
		})
	}
}

// TestRegoZanzibar implements the example (slightly improved) described in: https://storage.googleapis.com/pub-tools-public-publication-data/pdf/10683a8987dbf0c6d4edcafb9b4f05cc9de5974a.pdf
func TestRegoZanzibar(t *testing.T) {
	t.Parallel()

	// Test data:
	//
	// doc:readme#owner@10
	// group:eng#member@11
	// folder:X#viewer@12
	// doc:readme#viewer@group:eng#member
	// folder:A#parent@folder:X#...
	// doc:readme#parent@folder:A#...

	initSQL := `
        CREATE TABLE OWNER (OBJECT TEXT, USER TEXT, USERSET_OBJECT TEXT, USERSET_RELATION TEXT);
        CREATE TABLE EDITOR (OBJECT TEXT, USER TEXT, USERSET_OBJECT TEXT, USERSET_RELATION TEXT);
        CREATE TABLE MEMBER (OBJECT TEXT, USER TEXT, USERSET_OBJECT TEXT, USERSET_RELATION TEXT);
        CREATE TABLE VIEWER (OBJECT TEXT, USER TEXT, USERSET_OBJECT TEXT, USERSET_RELATION TEXT);
        CREATE TABLE PARENT (OBJECT TEXT, USER TEXT, USERSET_OBJECT TEXT, USERSET_RELATION TEXT);

        INSERT INTO OWNER(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('doc:readme', '10', '', '');
        INSERT INTO MEMBER(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('group:eng', '11', '', '');
        INSERT INTO VIEWER(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('folder:X', '12', '', '');
        INSERT INTO VIEWER(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('doc:readme', '', 'group:eng', 'MEMBER');
        INSERT INTO PARENT(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('folder:A', '', 'folder:X', '...');
        INSERT INTO PARENT(OBJECT, USER, USERSET_OBJECT, USERSET_RELATION) VALUES('doc:readme', '', 'folder:A', '...');
	`

	file := t.TempDir() + "/sqlite3.db"
	populate(t, file, initSQL)

	module := fmt.Sprintf(`
	package doc

	### Entrypoints

	is_owner { owner(input.resource, input.user) }
	is_editor { editor(input.resource, input.user) }
	is_viewer { viewer(input.resource, input.user) }

	### Owner
	#
	# relation { name: "owner" }

	owner(resource, user) {
	    query("owner", resource, user)
	}

	### Editor
	#
	#
	# relation {
	#   name: "editor"
	#   userset_rewrite {
	#     union {
	#	child { _this {} }
	#	child { computed_userset { relation: "owner" } }
	#     }
	#   }
	# }

	editor(resource, user) {
	    query("editor", resource, user)
	}

	editor(resource, user) {
	    owner(resource, user)
	}

	### Viewer
	#
	# relation {
	#   name: "viewer"
	#   userset_rewrite {
	#     union {
	#	child { _this {} }
	#	child { computed_userset { relation: "editor" } }
	#	child { tuple_to_userset {
	#	  tupleset { relation: "parent" }
	#	  computed_userset {
	#	    object: $TUPLE_USERSET_OBJECT # parent folder
	#	    relation: "viewer"
	#	  }
	#	} }
	#     }
	#   }
	# }

	viewer(resource, user) {
	    query("viewer", resource, user)
	}

	viewer(resource, user) {
	    editor(resource, user)
	}

	viewer(resource, user) {
	     p := parent(resource)
	     viewer1(p, user) # recurse
	}

	## SQL helpers

	query(table, resource, user) {
	    sql_send(table, resource)[_].user = user
	}

	query(table, resource, user) {
	    row := sql_send(table, resource)[_]
	    row.userset_relation != ""
	    query1(row.userset_relation, row.userset_object, user) # recurse
	}

	parent(resource) := folder {
	    row := sql_send("parent", resource)[0]
	    row.userset_relation = "..."
	    folder := row.userset_object
	}

	sql_send(table, resource) := rows {
	      result := sql.send({"driver": "sqlite", "data_source_name": "%s", "query": concat("", ["SELECT OBJECT, USER, USERSET_OBJECT, USERSET_RELATION FROM ", table, " WHERE OBJECT = $1"]), "args": [ resource ]})
	      rows := [ { "object": row[0], "user": row[1], "userset_object": row[2], "userset_relation": row[3] } | row := result.rows[_] ]
	}

	### fake viewer recursion (2 levels)

	viewer1(resource, user) {
	    query("viewer", resource, user)
	}

	viewer2(resource, user) {
	    query("viewer", resource, user)
	}

	viewer1(resource, user) {
	    editor(resource, user)
	}

	viewer2(resource, user) {
	    editor(resource, user)
	}

	viewer1(resource, user) {
	     p := parent(resource)
	     viewer2(p, user) # recurse
	}

	# fake query recursion (2 levels)

	query1(table, resource, user) {
	    sql_send(table, resource)[_].user = user
	}

	query2(table, resource, user) {
	    sql_send(table, resource)[_].user = user
	}

	query1(table, resource, user) {
	    row := sql_send(table, resource)[_]
	    row.userset_relation != ""
	    query2(row.userset_relation, row.userset_object, user) # recurse
	}
`, file)

	tests := []struct {
		note   string
		query  string
		input  string
		result string
	}{
		{
			note:   "owner is owner",
			query:  `doc/is_owner`,
			input:  `{"resource": "doc:readme", "user": "10"}`,
			result: `{{"result": true}}`,
		},
		{
			note:   "owner is editor",
			query:  `doc/is_editor`,
			input:  `{"resource": "doc:readme", "user": "10"}`,
			result: `{{"result": true}}`,
		},
		{
			note:   "editor is viewer",
			query:  `doc/is_viewer`,
			input:  `{"resource": "doc:readme", "user": "10"}`,
			result: `{{"result": true}}`,
		},
		{
			note:   "parent viewer is viewer",
			query:  `doc/is_viewer`,
			input:  `{"resource": "doc:readme", "user": "12"}`,
			result: `{{"result": true}}`,
		},
		{
			note:   "group member viewer is viewer",
			query:  `doc/is_viewer`,
			input:  `{"resource": "doc:readme", "user": "11"}`,
			result: `{{"result": true}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			var input interface{}
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}

			execute(t, nil, module, tc.query, tc.result, "", time.Now(), &input, 0, -1)
		})
	}
}

func execute(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, now time.Time, input *interface{}, expectedInterQueryCacheHits int, expectedPreparedQueries int) {
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
		Input:                  input,
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

	if expectedPreparedQueries >= 0 {
		if prepared := metrics.Counter(sqlSendPreparedQueries).Value().(uint64); prepared != uint64(expectedPreparedQueries) {
			tb.Fatalf("prepared queries: got %v prepared queries, wanted %v\n", prepared, expectedPreparedQueries)
		}
	}
}

var initSQL = `
        CREATE TABLE T1 (ID TEXT, VALUE TEXT);
        CREATE TABLE T2 (ID TEXT, VALUE TEXT);

        INSERT INTO T1(ID, VALUE) VALUES('A', 'B');
        INSERT INTO T2(ID, VALUE) VALUES('A1', 'B1');
        INSERT INTO T2(ID, VALUE) VALUES('A2', 'B2');
	`

func populate(tb testing.TB, file string, init string) {
	db, err := sql.Open("sqlite", file)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()

	if _, err = db.Exec(init); err != nil {
		tb.Fatal(err)
	}
}

func startMySQL(t *testing.T) backend {
	t.Helper()

	srv, err := mysql.Run(context.Background(), "mysql:9.0.0", testLogger(t))
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

	srv, err := postgres.Run(context.Background(), "postgres:16.3", testLogger(t),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatal(err)
	}

	connStr, err := srv.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("pgx", connStr)
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

func startMSSQL(t *testing.T) backend {
	t.Helper()
	password := "dEa9de93391d4312b18!520"

	srv, err := mssql.Run(context.Background(), "mcr.microsoft.com/mssql/server:2022-latest", testLogger(t),
		mssql.WithAcceptEULA(),
		mssql.WithPassword(password),
	)
	if err != nil {
		t.Fatal(err)
	}

	connStr, err := srv.ConnectionString(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlserver", connStr)
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

func testLogger(t testing.TB) testcontainers.CustomizeRequestOption {
	return testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
		req.Logger = testcontainers.TestLogger(t)
		return nil
	})
}
