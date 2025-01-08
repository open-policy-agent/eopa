//go:build use_opa_fork

package sql

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
)

type (
	// Options contains parameters that configure the SQL-based store.
	Options struct {
		Driver         string // SQL driver name
		DataSourceName string // SQL data source name
		Tables         []TableOpt
	}

	TableOpt struct {
		Path         storage.Path // Base path for the table (wild carded primary key included).
		Table        string
		KeyColumns   []string
		ValueColumns []ValueColumnOpt
		discovered   bool
	}

	ValueColumnOpt struct {
		Column string
		Type   string
		Path   string
	}
)

func (o Options) WithTables(tables []TableOpt) Options {
	o.Tables = tables
	return o
}

func (t *TableOpt) base() storage.Path {
	return t.Path[0 : len(t.Path)-len(t.KeyColumns)]
}

// AbsolutePath converts a table relative path to an absolute path.
func (t *TableOpt) AbsolutePath(path storage.Path) storage.Path {
	return append(t.base(), path...)
}

// RelativePath converts an absolute path to a table relative path.
func (t *TableOpt) RelativePath(path storage.Path) storage.Path {
	if len(path) <= len(t.base()) {
		return nil
	}

	return path[len(t.base()):]
}

func (t *TableOpt) DocPath(path storage.Path) storage.Path {
	if len(path) <= len(t.KeyColumns) {
		return nil
	}

	return path[len(t.KeyColumns):]
}

func (t *TableOpt) SelectQuery(path storage.Path) string {
	var q string

	for i := 0; i < len(t.KeyColumns) && i < len(path); i++ {
		if i == 0 {
			q = " WHERE "
		} else {
			q += " AND "
		}

		q += t.KeyColumns[i] + " = " + "$" + strconv.FormatInt(int64(i+1), 10)
	}

	columns := strings.Join(t.KeyColumns, ", ")

	for _, column := range t.ValueColumns {
		columns += ", " + column.Column
	}

	return "SELECT " + columns + " FROM " + t.Table + q
}

func (t *TableOpt) SelectArgs(path storage.Path) []interface{} {
	args := make([]interface{}, 0, len(t.KeyColumns))

	for i := 0; i < len(t.KeyColumns) && i < len(path); i++ {
		args = append(args, path[i])
	}

	return args
}

func (t *TableOpt) selectColumns() ([]interface{}, storage.Path, []interface{}, error) {
	path := make(storage.Path, len(t.KeyColumns))

	args := make([]interface{}, 0, len(path)+1)
	for i := 0; i < len(path); i++ {
		args = append(args, &path[i])
	}

	var values []interface{}
	for _, column := range t.ValueColumns {
		switch column.Type {
		case ColumnTypeJSON:
			var value []byte
			args = append(args, &value)
			values = append(values, &value)
		case ColumnTypeString:
			var value sql.NullString
			args = append(args, &value)
			values = append(values, &value)
		case ColumnTypeFloat64:
			var value sql.NullFloat64
			args = append(args, &value)
			values = append(values, &value)
		case ColumnTypeInt64:
			var value sql.NullInt64
			args = append(args, &value)
			values = append(values, &value)
		case ColumnTypeBool:
			var value sql.NullBool
			args = append(args, &value)
			values = append(values, &value)

		default:
			return nil, nil, nil, fmt.Errorf("unsupported column type: %v", column.Type)
		}
	}

	return args, path, values, nil
}

func (t *TableOpt) Scan(rows *sql.Rows) (storage.Path, interface{}, error) {
	args, path, values, err := t.selectColumns()
	if err != nil {
		return nil, nil, err
	}

	if err := rows.Scan(args...); err != nil {
		return nil, nil, err
	}

	var doc interface{}

	for i, column := range t.ValueColumns {
		var v interface{}
		var err error

		switch column.Type {
		case ColumnTypeJSON:
			bs := *values[i].(*[]byte)
			if len(bs) > 0 {
				err = deserialize(bs, &v)
			}
		case ColumnTypeString:
			if n := *values[i].(*sql.NullString); n.Valid {
				v = n.String
			} else {
				v = ""
			}
		case ColumnTypeBool:
			if n := *values[i].(*sql.NullBool); n.Valid {
				v = n.Bool
			}
		case ColumnTypeFloat64:
			if n := *values[i].(*sql.NullFloat64); n.Valid {
				v = n.Float64
			}
		case ColumnTypeInt64:
			if n := *values[i].(*sql.NullInt64); n.Valid {
				v = n.Int64
			}
		default:
			err = fmt.Errorf("unsupported column type: %v", column.Type)
		}

		if err != nil {
			return nil, nil, err
		}

		if column.Path == "" {
			doc = v
		} else {
			m, ok := doc.(map[string]interface{})
			if !ok {
				m = make(map[string]interface{})
				doc = m
			}

			key := storage.MustParsePath(column.Path)[0]
			m[key] = v
		}
	}

	return path, doc, nil
}

func (t *TableOpt) InsertQuery() string {
	var columns string
	var values string

	for i, column := range t.KeyColumns {
		if i > 0 {
			columns += ", "
			values += ", "
		}

		columns += column
		values += "$" + strconv.FormatInt(int64(i+1), 10)
	}

	for i, column := range t.ValueColumns {
		columns += ", " + column.Column
		values += ", $" + strconv.FormatInt(int64(len(t.KeyColumns)+i+1), 10)
	}

	return "INSERT INTO " + t.Table + " (" + columns + ") VALUES(" + values + ")"
}

func (t *TableOpt) InsertArgs(path storage.Path, value interface{}) ([]interface{}, error) {
	args := make([]interface{}, 0, len(t.KeyColumns)+1)

	i := 0
	for ; i < len(t.KeyColumns); i++ {
		args = append(args, path[i])
	}

	for _, column := range t.ValueColumns {
		var path storage.Path
		if column.Path != "" {
			path = storage.MustParsePath(column.Path)
		}
		v, err := Ptr(value, path)
		if err != nil && !storage.IsNotFound(err) {
			// Translate a non-existing field to NULL.
			return nil, err
		}

		var data interface{}

		switch column.Type {
		case ColumnTypeJSON:
			data, err = serialize(v)
		case ColumnTypeString:
			data, _ = v.(string)
		case ColumnTypeBool:
			data, _ = v.(bool)
		case ColumnTypeInt64:
			var bs []byte
			bs, err = json.Marshal(v)
			if err == nil {
				var n json.Number
				if err = util.NewJSONDecoder(bytes.NewReader(bs)).Decode(&n); err == nil {
					data, err = n.Int64()
				}
			}
		case ColumnTypeFloat64:
			var bs []byte
			bs, err = json.Marshal(v)
			if err == nil {
				var n json.Number
				if err = util.NewJSONDecoder(bytes.NewReader(bs)).Decode(&n); err == nil {
					data, err = n.Float64()
				}
			}
		default:
			err = fmt.Errorf("unsupported column type: %v", column.Type)
		}

		if err != nil {
			return nil, err
		}

		args = append(args, data)
	}

	return args, nil
}

func (t *TableOpt) DeleteQuery() string {
	var where string

	for i, column := range t.KeyColumns {
		if i > 0 {
			where += " AND "
		}

		where += column + " = $" + strconv.FormatInt(int64(i+1), 10)
	}

	return "DELETE FROM " + t.Table + " WHERE " + where
}

func (t *TableOpt) DeleteArgs(path storage.Path) []interface{} {
	args := make([]interface{}, len(t.KeyColumns))

	for i := 0; i < len(args); i++ {
		args[i] = path[i]
	}

	return args
}

func (t *TableOpt) Schema() string {
	schema := make([]string, 0, len(t.KeyColumns)+len(t.ValueColumns)+1)

	for _, column := range t.KeyColumns {
		schema = append(schema, column+" TEXT")
	}

	for _, column := range t.ValueColumns {
		switch column.Type {
		case ColumnTypeJSON, ColumnTypeString:
			schema = append(schema, column.Column+" TEXT")
		case ColumnTypeFloat64:
			schema = append(schema, column.Column+" REAL")
		case ColumnTypeInt64, ColumnTypeBool:
			schema = append(schema, column.Column+" INTEGER")
		default:
			panic(column.Type)
		}
	}

	schema = append(schema, "PRIMARY KEY("+strings.Join(t.KeyColumns, ", ")+")")

	return strings.Join(schema, ", ")
}
