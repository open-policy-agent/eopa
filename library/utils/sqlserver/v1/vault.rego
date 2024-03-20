# METADATA
# title: MS SQLServer helper method
# description: |
#  Utility wrapper for sql.send when talking to a MS SQLServer database.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { host: ..., port: ..., user: ..., password: ... }
#  at the key "sqlserver" in mount_path "secret".
package system.eopa.utils.sqlserver.v1.vault

import rego.v1
import data.system.eopa.utils.vault.v1.env as vault

send(query, args) := send_opts(query, args, {})

send_opts(query, args, opts) := sql.send(object.union(
        {
                "driver": "sqlserver",
                "data_source_name": _dsn(vault.secret(secret_path(true))),
                "query": query,
                "args": args,
        },
        opts,
))

_dsn(vault_data) :=	sprintf("sqlserver://%s:%s@%s:%s/%s", [user, pass, host, port, dbname]) if {
	user := vault_data.user
	pass := vault_data.password
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "1433")
}
else :=	sprintf("sqlserver://%s:%s/%s", [host, port, dbname]) if {
	dbname := vault_data.dbname
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "1433")
}

override.secret_path if false

secret_path(_) := override.secret_path if true
else := "secret/sqlserver"
