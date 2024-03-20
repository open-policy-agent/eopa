# METADATA
# title: MySQL helper method
# description: |
#  Utility wrapper for sql.send when talking to a MySQL database.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { host: ..., port: ..., user: ..., password: ... }
#  at the key "mysql" in mount_path "secret".
package system.eopa.utils.mysql.v1.vault

import rego.v1
import data.system.eopa.utils.vault.v1.env as vault

send(query, args) := send_opts(query, args, {})

send_opts(query, args, opts) := sql.send(object.union(
        {
                "driver": "mysql",
                "data_source_name": _dsn(vault.secret(secret_path(true))),
                "query": query,
                "args": args,
        },
        opts,
))

_dsn(vault_data) :=	sprintf("%s:%s@tcp(%s:%s)/%s?tls=%s", [user, pass, host, port, dbname, sslmode]) if {
	user := vault_data.user
	pass := vault_data.password
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "3306")
	sslmode := object.get(vault_data, "tls", "true")
}
else :=	sprintf("%s:%s@tcp(%s:%s)/%s?tls=%s", [host, port, dbname, sslmode]) if {
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "3306")
	sslmode := object.get(vault_data, "tls", "true")
}

override.secret_path if false

secret_path(_) := override.secret_path if true
else := "secret/mysql"
