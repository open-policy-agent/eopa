package builtins

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/lib/pq"
	"modernc.org/sqlite"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
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

	requiredKeys = ast.NewSet(ast.StringTerm("driver"), ast.StringTerm("data_source_name"), ast.StringTerm("query"))

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
		mu  sync.Mutex
		dbs map[databaseKey]*databaseConnection
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
		mu     sync.Mutex
		closed bool
		active int
		stmt   *sql.Stmt
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
	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(allowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameters(s): %v", invalidKeys)
	}

	missingKeys := requiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameters(s): %v", missingKeys)
	}

	driver, err := getRequestString(obj, "driver")
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

	if responseObj, ok, err := checkCaches(bctx, obj, interQueryCacheEnabled); ok {
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
			case *pq.Error:
				// See: https://github.com/lib/pq/blob/master/error.go
				e["severity"] = queryErr.Severity
				e["code"] = string(queryErr.Code)
				e["detail"] = queryErr.Detail
			case *sqlite.Error:
				// See: https://pkg.go.dev/modernc.org/sqlite#Error
				e["code"] = queryErr.Code()
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

	if err := insertCaches(bctx, obj, responseObj.(ast.Object), queryErr, interQueryCacheEnabled, ttl); err != nil {
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

func getRequestString(obj ast.Object, key string) (string, error) {
	if s, ok := obj.Get(ast.StringTerm(key)).Value.(ast.String); ok {
		return string(s), nil
	}

	return "", builtins.NewOperandErr(1, "'%s' must be string", key)
}

func getRequestBoolWithDefault(obj ast.Object, key string, def bool) (bool, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	if b, ok := v.Value.(ast.Boolean); ok {
		return bool(b), nil
	}

	return false, builtins.NewOperandErr(1, "'%s' must be bool", key)
}

func getRequestTimeoutWithDefault(obj ast.Object, key string, def time.Duration) (time.Duration, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	var timeout time.Duration
	switch t := v.Value.(type) {
	case ast.Number:
		timeoutInt, ok := t.Int64()
		if !ok {
			return timeout, fmt.Errorf("invalid timeout number value %v, must be int64", v)
		}
		return time.Duration(timeoutInt), nil

	case ast.String:
		// Support strings without a unit, treat them the same as just a number value (ns)
		var err error
		timeoutInt, err := strconv.ParseInt(string(t), 10, 64)
		if err == nil {
			return time.Duration(timeoutInt), nil
		}

		// Try parsing it as a duration (requires a supported units suffix)
		timeout, err = time.ParseDuration(string(t))
		if err != nil {
			return timeout, fmt.Errorf("invalid timeout value %v: %s", v, err)
		}
		return timeout, nil

	default:
		return timeout, builtins.NewOperandErr(1, "'timeout' must be one of {string, number} but got %s", ast.TypeName(t))
	}
}

func getRequestIntWithDefault(obj ast.Object, key string, def int64) (int64, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	switch n := v.Value.(type) {
	case ast.Number:
		i, ok := n.Int64()
		if !ok {
			return 0, fmt.Errorf("invalid number value %v, must be int64", v)
		}
		return i, nil

	case ast.String:
		var err error
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err == nil {
			return 0, fmt.Errorf("invalid string value %v, must be integer", v)
		}

		return i, nil

	default:
		return 0, builtins.NewOperandErr(1, "'int64' must be one of {string, number} but got %s", ast.TypeName(n))
	}
}

func checkCaches(bctx topdown.BuiltinContext, req ast.Object, interQueryCacheEnabled bool) (ast.Value, bool, error) {
	if interQueryCacheEnabled {
		if resp, ok, err := checkInterQueryCache(bctx, req); ok {
			return resp, true, err
		}
	}

	return checkIntraQueryCache(bctx, req)
}

func checkInterQueryCache(bctx topdown.BuiltinContext, req ast.Object) (ast.Value, bool, error) {
	cache := bctx.InterQueryBuiltinCache

	// TODO: Cache keys will not overlap with the http.send cache
	// keys because sql.send and http.send have each unique
	// required keys in their request objects. This is definitely
	// not an ideal arrangement to guarantee the isolation between
	// the two builtins.

	key := req
	serializedResp, found := cache.Get(key)
	if !found {
		return nil, false, nil
	}

	resp, err := serializedResp.(*interQueryCacheEntry).Unmarshal()
	if err != nil {
		return nil, true, err
	}

	if getCurrentTime(bctx).Before(resp.ExpiresAt) {
		bctx.Metrics.Counter(sqlSendInterQueryCacheHits).Incr()
		resp, err := resp.FormatToAST()
		return resp, true, err
	}

	// No valid entry found.

	return nil, false, nil
}

func checkIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object) (ast.Value, bool, error) {
	if v := getIntraQueryCache(bctx).Get(req); v != nil {
		// It's safe not to clone the response as the VM will
		// convert the AST types into its internal
		// representation anyway.
		return v.Response, true, v.Error
	}

	return nil, false, nil
}

func getIntraQueryCache(bctx topdown.BuiltinContext) *intraQueryCache {
	raw, ok := bctx.Cache.Get(sqlSendBuiltinCacheKey)
	if !ok {
		c := newIntraQueryCache()
		bctx.Cache.Put(sqlSendBuiltinCacheKey, c)
		return c
	}

	return raw.(*intraQueryCache)
}

func getCurrentTime(bctx topdown.BuiltinContext) time.Time {
	var current time.Time

	value, err := ast.JSON(bctx.Time.Value)
	if err != nil {
		return current
	}

	valueNum, ok := value.(json.Number)
	if !ok {
		return current
	}

	valueNumInt, err := valueNum.Int64()
	if err != nil {
		return current
	}

	current = time.Unix(0, valueNumInt).UTC()
	return current
}

func newInterQueryCacheEntry(bctx topdown.BuiltinContext, resp ast.Object, ttl time.Duration) (*interQueryCacheEntry, error) {
	data, err := newInterQueryCacheData(bctx, resp, ttl)
	if err != nil {
		return nil, err
	}

	return data.Marshal()
}

func (e interQueryCacheEntry) SizeInBytes() int64 {
	return int64(len(e.Data))
}

func (e interQueryCacheEntry) Unmarshal() (*interQueryCacheData, error) {
	var data interQueryCacheData
	err := util.UnmarshalJSON(e.Data, &data)
	return &data, err
}

func (e interQueryCacheEntry) Clone() (cache.InterQueryCacheValue, error) {
	return e, nil
}

func newInterQueryCacheData(bctx topdown.BuiltinContext, resp ast.Object, ttl time.Duration) (*interQueryCacheData, error) {
	r, err := ast.JSONWithOpt(resp, ast.JSONOpt{})
	if err != nil {
		return nil, err
	}

	return &interQueryCacheData{
		Response:  r,
		ExpiresAt: getCurrentTime(bctx).Add(ttl),
	}, nil
}

func (c *interQueryCacheData) FormatToAST() (ast.Value, error) {
	return ast.InterfaceToValue(c.Response)
}

func (c *interQueryCacheData) Marshal() (*interQueryCacheEntry, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	return &interQueryCacheEntry{Data: b}, nil
}

func (*interQueryCacheData) SizeInBytes() int64 {
	return 0
}

func newIntraQueryCache() *intraQueryCache {
	return &intraQueryCache{
		entries: util.NewHashMap(
			func(k1, k2 util.T) bool {
				return k1.(ast.Value).Compare(k2.(ast.Value)) == 0
			},
			func(k util.T) int {
				return k.(ast.Value).Hash()
			}),
	}
}

func (cache *intraQueryCache) Get(key ast.Value) *intraQueryCacheEntry {
	if v, ok := cache.entries.Get(key); ok {
		v := v.(intraQueryCacheEntry)
		return &v
	}

	return nil
}

func (cache *intraQueryCache) PutResponse(key ast.Value, response ast.Object) {
	cache.entries.Put(key, intraQueryCacheEntry{Response: response})
}

func (cache *intraQueryCache) PutError(key ast.Value, err error) {
	cache.entries.Put(key, intraQueryCacheEntry{Error: err})
}

func insertCaches(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, queryErr error, interQueryCacheEnabled bool, ttl time.Duration) error {
	if queryErr == nil && interQueryCacheEnabled {
		// Only cache successful queries for across queries;
		// currently we can't separate between transient
		// errors (e.g., network issues) and persistent errors
		// (e.g., query syntax). Hence, it's impossible to
		// know when queries actually warrant for retries and
		// should not be cached.
		if err := insertInterQueryCache(bctx, req, resp, ttl); err != nil {
			return err
		}
	}

	// Within a query we expect deterministic results, hence cache
	// errors too.

	insertIntraQueryCache(bctx, req, resp, queryErr)
	return nil
}

func insertInterQueryCache(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, ttl time.Duration) error {
	entry, err := newInterQueryCacheEntry(bctx, resp, ttl)
	if err != nil {
		return err
	}

	bctx.InterQueryBuiltinCache.Insert(req, entry)
	return nil
}

func insertIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, queryErr error) {
	if queryErr == nil {
		getIntraQueryCache(bctx).PutResponse(req, resp)
	} else {
		getIntraQueryCache(bctx).PutError(req, queryErr)
	}
}

func init() {
	topdown.RegisterBuiltinFunc(sqlSendName, builtinSQLSend)
}
