// see marker below
//go:build use_opa_fork

package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
)

var (
	ErrInvalidTableName   = errors.New("invalid table name")
	ErrInvalidTablePath   = errors.New("invalid storage path")
	ErrInvalidTableColumn = errors.New("invalid table column")
)

type (
	cfg struct {
		Driver         string     `json:"driver"`
		DataSourceName string     `json:"data_source_name"`
		Tables         []Table    `json:"tables"`
		Discovery      *Discovery `json:"discovery"`
	}

	Table struct {
		Path       string        `json:"path"`        // Tables path in the namespace.
		Table      string        `json:"table"`       // Table name
		PrimaryKey []string      `json:"primary_key"` // Column names
		Values     []ValueColumn `json:"values"`      // Value columns
	}

	ValueColumn struct {
		Column string `json:"column"`
		Path   string `json:"path"`
		Type   string `json:"type"`
	}

	Discovery struct {
		Schema string `json:"schema"`
		Path   string `json:"path"`
	}
)

const (
	ColumnTypeJSON    = "json"
	ColumnTypeString  = "string"
	ColumnTypeBool    = "bool"
	ColumnTypeFloat64 = "float64"
	ColumnTypeInt64   = "int64"
)

// OptionsFromConfig parses the passed config, extracts the disk storage
// settings, validates it, and returns a *Options struct pointer on success.
func OptionsFromConfig(ctx context.Context, raw []byte, id string) (*Options, error) {
	parsedConfig, err := config.ParseConfig(raw, id)
	if err != nil {
		return nil, err
	}

	var cfgRaw []byte
	if parsedConfig.Extra != nil {
		cfgRaw = parsedConfig.Extra["sql"]
	}
	if len(cfgRaw) == 0 {
		return nil, nil
	}

	var c cfg
	if err := util.Unmarshal(cfgRaw, &c); err != nil {
		return nil, err
	}

	opts := Options{
		Driver:         c.Driver,
		DataSourceName: c.DataSourceName,
	}

	for _, table := range c.Tables {
		path, ok := storage.ParsePath(table.Path)
		if !ok {
			return nil, fmt.Errorf("table path '%v': %w", table.Path, ErrInvalidTablePath)
		}

		completePath := make(storage.Path, len(path))
		copy(completePath, path)
		for n := len(table.PrimaryKey); n > 0; n-- {
			completePath = append(completePath, pathWildcard)
		}

		if table.Table == "" {
			return nil, fmt.Errorf("table missing name '%v': %w", path, ErrInvalidTablePath)
		}

		t := TableOpt{Path: completePath, Table: table.Table, KeyColumns: table.PrimaryKey}

		for _, value := range table.Values {
			var typ string

			switch value.Type {
			case ColumnTypeJSON, ColumnTypeString, ColumnTypeBool, ColumnTypeInt64:
				typ = value.Type
			default:
				return nil, fmt.Errorf("table path '%v': %w", path, ErrInvalidTableColumn)
			}

			t.ValueColumns = append(t.ValueColumns, ValueColumnOpt{Column: value.Column, Type: typ, Path: value.Path})
		}

		opts.Tables = append(opts.Tables, t)
	}

	// Auto-discover tables as configured.

	if discovery := c.Discovery; discovery != nil {
		db, err := sql.Open(opts.Driver, opts.DataSourceName)
		if err != nil {
			return nil, wrapError(err)
		}

		path, ok := storage.ParsePath(discovery.Path)
		if !ok {
			return nil, fmt.Errorf("table path '%v': %w", discovery.Path, ErrInvalidTablePath)
		}

		if discovery.Schema == "" {
			discovery.Schema = "public"
		}

		type candidate struct {
			schema      string
			name        string
			columns     map[string]string
			constraint  string
			primaryKeys []string
		}

		discovered := make(map[[2]string]*candidate)

		// 1. Query the columns table:
		//    Identify the table name, column names,
		//    Filter by the schemas (default public), table names

		rows, err := db.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS")
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var tableSchema string
			var tableName string
			var columnName string
			var dataType string

			if err := rows.Scan(&tableSchema, &tableName, &columnName, &dataType); err != nil {
				rows.Close()
				return nil, err
			}

			if discovery.Schema != tableSchema {
				continue
			}

			key := [2]string{tableSchema, tableName}
			d, ok := discovered[key]
			if !ok {
				d = &candidate{schema: tableSchema, name: tableName, columns: make(map[string]string)}
				discovered[key] = d
			}

			d.columns[columnName] = dataType
		}

		if err := rows.Err(); err != nil {
			return nil, err
		}

		// 2. Query the constraints table to identify the primary key
		//    constraint for the chosen table

		rows, err = db.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME, CONSTRAINT_NAME FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS WHERE CONSTRAINT_TYPE = 'PRIMARY KEY'")
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var tableSchema string
			var tableName string
			var constraintName string

			if err := rows.Scan(&tableSchema, &tableName, &constraintName); err != nil {
				rows.Close()
				return nil, err
			}

			key := [2]string{tableSchema, tableName}
			d, ok := discovered[key]
			if !ok {
				continue
			}

			d.constraint = constraintName
		}

		if err := rows.Err(); err != nil {
			return nil, err
		}

		// 3. Query the key usage to identify the columns of the
		//    primary key

		rows, err = db.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME, CONSTRAINT_NAME, COLUMN_NAME FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE ORDER BY ORDINAL_POSITION")
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var tableSchema string
			var tableName string
			var constraintName string
			var columnName string

			if err := rows.Scan(&tableSchema, &tableName, &constraintName, &columnName); err != nil {
				rows.Close()
				return nil, err
			}

			key := [2]string{tableSchema, tableName}
			d, ok := discovered[key]
			if !ok {
				continue
			}

			if d.constraint != constraintName {
				continue
			}

			d.primaryKeys = append(d.primaryKeys, columnName)
		}

		if err := rows.Err(); err != nil {
			return nil, err
		}

		// 4. Import the tables discovered.

		for _, d := range discovered {
			for _, column := range d.primaryKeys {
				delete(d.columns, column)
			}

			var values []ValueColumnOpt
			for column, kind := range d.columns {
				var t string

				switch kind {
				case "ARRAY", "bigint", "double precision":
					t = ColumnTypeJSON
				case "boolean":
					t = ColumnTypeBool
				case "jsonb":
					t = ColumnTypeJSON
				case "bytea", "text", "uuid", "timestamp without time zone":
					t = ColumnTypeString
				default:
					return nil, fmt.Errorf("unsupported column type: %v", kind)
				}

				values = append(values, ValueColumnOpt{Column: column, Type: t, Path: storage.Path{column}.String()})
			}

			p := append(path, d.name)
			for range d.primaryKeys {
				p = append(p, pathWildcard)
			}

			opts.Tables = append(opts.Tables, TableOpt{Path: p, Table: d.schema + "." + d.name, KeyColumns: d.primaryKeys, ValueColumns: values, discovered: true})
		}
	}

	return &opts, nil
}
