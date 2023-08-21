# METADATA
# title: PostgreSQL helper method
# description: |
#  Utility wrapper for sql.send when talking to a PostgreSQL database.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { host: ..., port: ..., user: ..., password: ... }
#  at the key "postgres" in mount_path "secret"
package system.eopa.utils.postgres.v1.vault

import future.keywords.if
import data.system.eopa.utils.vault.v1.env as vault

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
	vault_data := vault.secret("secret/postgres")
	dsn := _dsn(vault_data)
	cache := object.get(opts, "cache", false)
	cache_duration := object.get(opts, "cache_duration", "60s")
	raise_error := object.get(opts, "raise_error", true)
}

_dsn(vault_data) :=	sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s", [user, pass, host, port, dbname, sslmode]) if {
	user := vault_data.user
	pass := vault_data.password
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "5432")
	sslmode := object.get(vault_data, "sslmode", "require")
}
else := sprintf("postgresql://%s:%s/%s?sslmode=%s", [host, port, dbname, sslmode]) if {
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "5432")
	sslmode := object.get(vault_data, "sslmode", "require")
}
