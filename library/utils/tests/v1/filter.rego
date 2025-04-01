# METADATA
# title: Data filtering test helper method
# description: |
#  Utility setup for testing data filtering policies.
package system.eopa.utils.tests.v1

filter.helper(query, select, tables, opts) := results if {
	db := object.get(opts, "db", ":memory:")
	debug := object.get(opts, "debug", false)
	setup_tables(debug, db, tables)

	mappings := object.get(opts, "mappings", {})
	conditions = rego.compile({
		"query": query,
		"target": "sql+sqlite-internal",
		"mappings": {"sqlite-internal": mappings},
	})
	print_debug(debug, "rego.compile response: %v", [conditions])
	results := list(debug, db, select, conditions.query)
	print_debug(debug, "list response: %v", [results])
	drop_tables(debug, db, tables)
}

setup_tables(debug, db, tables) if {
	some name, rows in tables
	create_table(debug, db, name, rows)
	fill_table(debug, db, name, rows, count(rows) - 1)
}

create_table(debug, db, name, rows) := sql.send(sql_query(debug, db, q)) if {
	columns := object.keys(rows[0])
	cols := concat(", ", {sprintf("%s ANY", [col]) | some col in columns})
	q := sprintf("CREATE TABLE %s (%s)", [name, cols])
}

fill_table(debug, db, name, rows, done) if {
	columns := object.keys(rows[0])
	cols := concat(", ", columns)
	some i, row in rows
	values := [to_val(val) | some val in row]
	val0 := concat(", ", values)
	q := sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *", [name, cols, val0])
	res := sql.send(sql_query(debug, db, q))
	count(res.rows) == 1
	i == done # iterate until done
}

# only string values need quoting
to_val(x) := sprintf("'%v'", [x]) if is_string(x)

else := sprintf("%v", [x])

drop_tables(debug, db, tables) if {
	some name, _ in tables
	drop_table(debug, db, name)
}

drop_table(debug, db, name) := sql.send(sql_query(debug, db, q)) if {
	db == ":memory:"
	q := sprintf("DROP TABLE %s", [name])
} else := true

list(debug, db, select, where) := res.rows if {
	q := build_query(select, where)
	res := sql.send(sql_query(debug, db, q))
}

sql_query(debug, db, q) := {
	"driver": "sqlite",
	"data_source_name": db,
	"query": q,
	"row_object": true,
	"raise_error": false, # return errors in-band
} if {
	print_debug(debug, "executing query %s against %s", [q, db])
}

build_query(select, where) := concat(" ", [select, where]) if not contains(lower(select), " where ")

build_query(select, where) := combined if {
	contains(lower(select), " where ")
	where_sans_where := substring(where, 6, -1) # drop "WHERE "
	combined := sprintf("%s AND (%s)", [select, where_sans_where])
}

print_debug(debug, format, args) if {
	debug # regal ignore:redundant-existence-check
	print(sprintf(format, args)) # regal ignore:dubious-print-sprintf,print-or-trace-call
} else := true
