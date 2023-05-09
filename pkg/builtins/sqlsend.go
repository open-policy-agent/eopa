package builtins

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/lib/pq"  // Include the PostgreSQL driver to the binary.
	_ "modernc.org/sqlite" // Include the SQLite3 driver to the binary.

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
)

const (
	sqlSendName = "sql.send"
)

var (
	databases    = databasePool{dbs: make(map[[2]string]*databaseConnection)}
	allowedKeys  = ast.NewSet(ast.StringTerm("driver"), ast.StringTerm("data_source_name"), ast.StringTerm("query"), ast.StringTerm("args"))
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
)

type (
	databasePool struct {
		mu  sync.Mutex
		dbs map[[2]string]*databaseConnection
	}

	databaseConnection struct {
		mu         sync.Mutex
		db         *sql.DB
		statements map[string]*sql.Stmt
	}
)

func init() {
	ast.RegisterBuiltin(sqlSend)
	topdown.RegisterBuiltinFunc(sqlSendName, builtinSQLSend)
}

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

	query, err := getRequestString(obj, "query")
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	// TODO: Improve error handling, both to allow control over
	// error handling ("raise_error" request parameter as well as
	// to separate between types of errors (invalid queries,
	// connectivity errors, etc.)

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

	db, err := databases.Get(bctx.Context, driver, dsn)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	rows, err := db.Query(bctx.Context, query, args...)
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	columns, err := rows.Columns()
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	result := make([]interface{}, 0)

	for rows.Next() {
		row := make([]interface{}, 0, len(columns))
		for range columns {
			var value interface{}
			row = append(row, &value)
		}

		if err := rows.Scan(row...); err != nil {
			return handleBuiltinErr(sqlSendName, bctx.Location, err)
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	responseObj, err := ast.InterfaceToValue(map[string]interface{}{
		"rows": result,
	})
	if err != nil {
		return handleBuiltinErr(sqlSendName, bctx.Location, err)
	}

	return iter(ast.NewTerm(responseObj))
}

func (p *databasePool) Get(_ context.Context, driver string, dsn string) (*databaseConnection, error) {
	p.mu.Lock()

	db, ok := p.dbs[[2]string{driver, dsn}]
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
	existing, ok := p.dbs[[2]string{driver, dsn}]
	if ok {
		p.mu.Unlock()

		if err := newDb.Close(); err != nil {
			return nil, err
		}

		return existing, nil
	}

	defer p.mu.Unlock()

	db = &databaseConnection{db: newDb, statements: make(map[string]*sql.Stmt)}
	p.dbs[[2]string{driver, dsn}] = db
	return db, nil
}

func (c *databaseConnection) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.mu.Lock()
	stmt, ok := c.statements[query]
	c.mu.Unlock()

	if !ok {
		var err error
		stmt, err = c.db.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		if existing, ok := c.statements[query]; ok {
			c.mu.Unlock()

			stmt.Close()
			stmt = existing
		} else {
			c.statements[query] = stmt
			c.mu.Unlock()
		}
	}

	return stmt.QueryContext(ctx, args...)
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
