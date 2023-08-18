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

send(query, args) := sql.send({
	"driver": "postgres",
	"data_source_name": dsn,
	"query": query,
	"args": args,
}) if {
	vault_data := vault.data("secret/postgres")
	user := vault_data.user
	pass := vault_data.password
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "5432")
	sslmode := object.get(vault_data, "sslmode", "require")
	dsn := sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s", [user, pass, host, port, dbname, sslmode])
}
else := sql.send({
	"driver": "postgres",
	"data_source_name": dsn,
	"query": query,
	"args": args,
}) if {
	vault_data := vault.data("secret/postgres")
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "5432")
	sslmode := object.get(vault_data, "sslmode", "require")
	dsn := sprintf("postgresql://%s:%s/%s?sslmode=%s", [host, port, dbname, sslmode])
}
