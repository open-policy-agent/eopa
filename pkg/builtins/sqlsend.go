package builtins

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql compatible driver for pgx
	"modernc.org/sqlite"

	lru "github.com/hashicorp/golang-lru/v2"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

const (
	sqlSendName = "sql.send"
	// sqlSendBuiltinCacheKey is the key in the builtin context cache that
	// points to the sql.send() specific intra-query cache resides at.
	sqlSendBuiltinCacheKey         sqlSendKey = "SQL_SEND_CACHE_KEY"
	interQueryCacheDurationDefault            = 60 * time.Second
	maxPreparedStatementsDefault              = 128
)

var (
	databases   = databasePool{dbs: make(map[databaseKey]*databaseConnection)}
	allowedKeys = ast.NewSet(
		ast.StringTerm("args"),
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("connection_max_idle_time"),
		ast.StringTerm("connection_max_life_time"),
		ast.StringTerm("data_source_name"),
		ast.StringTerm("driver"),
		ast.StringTerm("max_idle_connections"),
		ast.StringTerm("max_open_connections"),
		ast.StringTerm("max_prepared_statements"),
		ast.StringTerm("query"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("row_object"),
	)

	requiredKeys     = ast.NewSet(ast.StringTerm("driver"), ast.StringTerm("data_source_name"), ast.StringTerm("query"))
	supportedDrivers = ast.NewSet(ast.StringTerm("postgres"), ast.StringTerm("mysql"), ast.StringTerm("sqlite"))

	// Marked non-deterministic because SQL query results can be non-deterministic.
	sqlSend = &ast.Builtin{
		Name:        sqlSendName,
		Description: "Returns query result rows to the given SQL query.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))),
		),
		Nondeterministic: true,
	}

	sqlSendLatencyMetricKey    = "rego_builtin_sql_send"
	sqlSendInterQueryCacheHits = sqlSendLatencyMetricKey + "_interquery_cache_hits"
	sqlSendPreparedQueries     = sqlSendLatencyMetricKey + "_prepared_queries"
)

type (
	databasePool struct {
		dbs map[databaseKey]*databaseConnection
		mu  sync.Mutex
	}

	databaseKey struct {
		driver                string
		dsn                   string
		maxOpenConnections    int64
		maxIdleConnections    int64
		connectionMaxIdleTime time.Duration
		connectionMaxLifetime time.Duration
		maxPreparedStatements int64
	}

	databaseConnection struct {
		db         *sql.DB
		statements *lru.Cache[string, *databaseStmt]
	}

	databaseStmt struct {
		stmt   *sql.Stmt
		active int
		mu     sync.Mutex
		closed bool
	}

	intraQueryCache struct {
		entries *util.HashMap
	}

	intraQueryCacheEntry struct {
		Response ast.Object
		Error    error
	}

	interQueryCacheEntry struct {
		Data []byte
	}

	interQueryCacheData struct {
		Response  interface{} `json:"response"`
		ExpiresAt time.Time   `json:"expires_at"`
	}

	sqlSendKey string
)

func builtinSQLSend(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(sqlSendName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(allowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := requiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	driver, err := getRequestString(obj, "driver")
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	driver, err = validateDriver(driver)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	dsn, err := getRequestString(obj, "data_source_name")
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	maxOpenConnections, err := getRequestIntWithDefault(obj, "max_open_connections", 0)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	maxIdleConnections, err := getRequestIntWithDefault(obj, "max_idle_connections", 2)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	connectionMaxIdleTime, err := getRequestTimeoutWithDefault(obj, "connection_max_idle_time", 0)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	connectionMaxLifetime, err := getRequestTimeoutWithDefault(obj, "connection_max_life_time", 0)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	maxPreparedStatements, err := getRequestIntWithDefault(obj, "max_prepared_statements", maxPreparedStatementsDefault)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}
	if maxPreparedStatements <= 0 {
		maxPreparedStatements = 1
	}

	query, err := getRequestString(obj, "query")
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}
	span.SetAttributes(attribute.String("query", query))

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	rowObject, err := getRequestBoolWithDefault(obj, "row_object", false)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	// TODO: Improve error handling to allow separation between
	// types of errors (invalid queries, connectivity errors,
	// etc.)

	var args []interface{}
	if v := obj.Get(ast.StringTerm("args")); v != nil {
		a, err := ast.JSON(v.Value)
		if err != nil {
			return handleBuiltinErr(sqlSendName, bctx.Location, err)
		}

		arr, ok := a.([]interface{})
		if !ok {
			return builtins.NewOperandErr(1, "'%s' must be array", "args")
		}

		args = arr
	}

	bctx.Metrics.Timer(sqlSendLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, obj, interQueryCacheEnabled, sqlSendBuiltinCacheKey, sqlSendInterQueryCacheHits); ok {
		if err != nil {
			return handleBuiltinErr(sqlSendName, bctx.Location, err)
		}

		return iter(ast.NewTerm(responseObj))
	}

	result, queryErr := func() ([]interface{}, error) {
		db, err := databases.Get(bctx.Context, driver, dsn, maxOpenConnections, maxIdleConnections, connectionMaxIdleTime, connectionMaxLifetime, maxPreparedStatements)
		if err != nil {
			return nil, err
		}

		rows, err := db.Query(bctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		columnTypes, err := rows.ColumnTypes()
		if err != nil {
			return nil, err
		}

		result := make([]interface{}, 0)

		for rows.Next() {
			row := make([]interface{}, 0, len(columns))
			for _, column := range columnTypes {
				// MySQL driver returns all textual columns as []byte and needs a little help to convert them to a string.
				// Fortunately, these types should be universally strings across all drivers so no need to check driver type.
				//
				// TODO: There may be other (non-textual) types that require similar treatment.
				switch column.DatabaseTypeName() {
				case "VARCHAR", "TEXT", "LONGTEXT", "TINYTEXT", "MEDIUMTEXT": //  See fields.go of github.com/go-sql-driver/mysql for the supported textual types.
					var value string
					row = append(row, &value)
				default:
					var value interface{}
					row = append(row, &value)
				}
			}

			if err := rows.Scan(row...); err != nil {
				return nil, err
			}

			if rowObject {
				obj := make(map[string]interface{}, len(columns))
				for i, column := range columns {
					obj[column] = row[i]
				}
				result = append(result, obj)
			} else {
				result = append(result, row)
			}
		}

		return result, rows.Err()
	}()

	m := map[string]interface{}{
		"rows": result,
	}

	if queryErr != nil {
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.

			e := map[string]interface{}{
				"message": string(queryErr.Error()),
			}

			switch queryErr := queryErr.(type) {
			case *mysql.MySQLError:
				// See: https://github.com/go-sql-driver/mysql/blob/master/errors.go
				e["number"] = int(queryErr.Number)
			case *sqlite.Error:
				// See: https://pkg.go.dev/modernc.org/sqlite#Error
				e["code"] = queryErr.Code()
			default:
				var perr *pgconn.PgError
				if errors.As(queryErr, &perr) {
					// See: https://pkg.go.dev/github.com/jackc/pgconn#PgError
					e["severity"] = perr.Severity
					e["code"] = perr.Code
					e["detail"] = perr.Detail
				}
			}

			m["error"] = e
			queryErr = nil
		} else {
			return handleBuiltinErr(sqlSendName, bctx.Location, queryErr)
		}
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	if err := insertCaches(bctx, obj, responseObj.(ast.Object), queryErr, interQueryCacheEnabled, ttl, sqlSendBuiltinCacheKey); err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	bctx.Metrics.Timer(sqlSendLatencyMetricKey).Stop()

	return iter(ast.NewTerm(responseObj))
}

func (p *databasePool) Get(_ context.Context, driver string, dsn string, maxOpenConnections int64, maxIdleConnections int64, connectionMaxIdleTime time.Duration, connectionMaxLifetime time.Duration, maxPreparedStatements int64) (*databaseConnection, error) {
	p.mu.Lock()

	key := databaseKey{
		driver,
		dsn,
		maxOpenConnections,
		maxIdleConnections,
		connectionMaxIdleTime,
		connectionMaxLifetime,
		maxPreparedStatements,
	}
	db, ok := p.dbs[key]
	if ok {
		p.mu.Unlock()
		return db, nil
	}

	p.mu.Unlock()

	newDb, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	existing, ok := p.dbs[key]
	if ok {
		p.mu.Unlock()

		if err := newDb.Close(); err != nil {
			return nil, err
		}

		return existing, nil
	}

	newDb.SetMaxOpenConns(convertToInt(maxOpenConnections))
	newDb.SetMaxIdleConns(convertToInt(maxIdleConnections))
	newDb.SetConnMaxIdleTime(connectionMaxIdleTime)
	newDb.SetConnMaxLifetime(connectionMaxLifetime)

	defer p.mu.Unlock()

	db = &databaseConnection{db: newDb}
	db.statements, err = lru.NewWithEvict[string, *databaseStmt](convertToInt(maxPreparedStatements), db.evict)
	if err != nil {
		return nil, err
	}

	p.dbs[key] = db
	return db, nil
}

func convertToInt(n int64) int {
	if n > math.MaxInt {
		return math.MaxInt
	} else if n < math.MinInt {
		return math.MinInt
	}

	return int(n)
}

func (c *databaseConnection) Query(bctx topdown.BuiltinContext, query string, args ...any) (*sql.Rows, error) {
	ctx := bctx.Context

	for ctx.Err() == nil {
		stmt, ok := c.statements.Get(query)
		if ok && stmt.Acquire(1) {
			defer stmt.Release(1, false)
			return stmt.QueryContext(ctx, args...)
		}

		s, err := c.db.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}

		bctx.Metrics.Counter(sqlSendPreparedQueries).Incr()

		stmt = &databaseStmt{closed: false, stmt: s}
		stmt.Acquire(1) // Always succeeds.

		if _, exists, _ := c.statements.PeekOrAdd(query, stmt); exists {
			// Check the cache again, it should still have a
			// statement there unless it was just evicted.
			stmt.Release(1, true)
			continue
		}

		// New statement is guaranteed not to be closed yet
		// since it was inserted into the cache as in active
		// use.

		defer stmt.Release(1, false)
		return stmt.QueryContext(ctx, args...)
	}

	return nil, ctx.Err()
}

func (*databaseConnection) evict(_ string, stmt *databaseStmt) {
	stmt.Release(0, true)
}

func (s *databaseStmt) QueryContext(ctx context.Context, args ...interface{}) (*sql.Rows, error) {
	return s.stmt.QueryContext(ctx, args...)
}

func (s *databaseStmt) Acquire(n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	s.active += n
	return true
}

func (s *databaseStmt) Release(n int, close bool) {
	var closeStmt *sql.Stmt

	s.mu.Lock()

	s.active -= n

	if close {
		s.closed = true
	}

	if s.closed && s.active == 0 {
		closeStmt = s.stmt
		s.stmt = nil
	}

	s.mu.Unlock()

	if closeStmt != nil {
		closeStmt.Close() // TODO: Anything to do with the error?
	}
}

func handleBuiltinErr(name string, loc *ast.Location, err error) error {
	switch err := err.(type) {
	case builtins.ErrOperand:
		return &topdown.Error{
			Code:     topdown.TypeErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
	default:
		return &topdown.Error{
			Code:     topdown.BuiltinErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
	}
}

func validateDriver(d string) (string, error) {
	switch d {
	case "postgres":
		d = "pgx"
	case "mysql", "sqlite": // OK
	default:
		return "", builtins.NewOperandErr(1, "unknown driver %s, must be one of %v", d, supportedDrivers)
	}
	return d, nil
}

func init() {
	topdown.RegisterBuiltinFunc(sqlSendName, builtinSQLSend)
}
