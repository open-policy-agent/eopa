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

send(query, args) := send_opts(query, args, {})

send_opts(query, args, opts) := sql.send({
	"driver": "postgres",
	"data_source_name": dsn,
	"query": query,
	"args": args,
	"cache": cache,
	"cache_duration": cache_duration,
	"raise_error": raise_error,
}) if {
    dsn := _dsn(opa.runtime().env)
   	cache := object.get(opts, "cache", false)
	cache_duration := object.get(opts, "cache_duration", "60s")
	raise_error := object.get(opts, "raise_error", true)
}

_dsn(env) := sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s", [user, pass, host, port, dbname, sslmode]) if {
	user := env.PGUSER
	pass := env.PGPASSWORD
	dbname := env.PGDBNAME
	host := object.get(env, "PGHOST", "localhost")
	port := object.get(env, "PGPORT", "5432")
	sslmode := object.get(env, "PGSSLMODE", "require")
}

else := sprintf("postgresql://%s:%s/%s?sslmode=%s", [host, port, dbname, sslmode]) if {
	dbname := env.PGDBNAME
	host := object.get(env, "PGHOST", "localhost")
	port := object.get(env, "PGPORT", "5432")
	sslmode := object.get(env, "PGSSLMODE", "require")
}
