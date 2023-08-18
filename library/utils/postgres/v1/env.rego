# METADATA
# title: PostgreSQL helper method
# description: |
#  Utility wrapper for sql.send when talking to a PostgreSQL database.
#  Addresses and credentials are taken from the environment, in the
#  same way that `psql` does it: PGHOST, PGPORT, PGDATABASE, ...
# related_resources:
# - https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNECT-HOST
package system.eopa.utils.postgres.v1.env

import future.keywords.if

send(query, args) := sql.send({
	"driver": "postgres",
	"data_source_name": dsn,
	"query": query,
	"args": args,
}) if {
    env := opa.runtime().env
	user := env.PGUSER
	pass := env.PGPASSWORD
	dbname := env.PGDBNAME
	host := object.get(env, "PGHOST", "localhost")
	port := object.get(env, "PGPORT", "5432")
	sslmode := object.get(env, "PGSSLMODE", "require")
	dsn := sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s", [user, pass, host, port, dbname, sslmode])
}

else := sql.send({
	"driver": "postgres",
	"data_source_name": dsn,
	"query": query,
	"args": args,
}) if {
    env := opa.runtime().env
	dbname := env.PGDBNAME
	host := object.get(env, "PGHOST", "localhost")
	port := object.get(env, "PGPORT", "5432")
	sslmode := object.get(env, "PGSSLMODE", "require")
	dsn := sprintf("postgresql://%s:%s/%s?sslmode=%s", [host, port, dbname, sslmode])
}
