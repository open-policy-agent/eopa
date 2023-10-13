# METADATA
# title: MongoDB helper methods
# description: |
#  Utility wrapper for mongodb.send: `find` and `find_one`.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { host: ..., port: ..., user: ..., password: ... }
#  at the key "mongodb" in mount_path "secret"
# related_resources:
# - https://www.mongodb.com/docs/manual/reference/connection-string/
package system.eopa.utils.mongodb.v1.vault

import future.keywords.if
import data.system.eopa.utils.vault.v1.env as vault

# TODO(sr): treat database like DBNAME in PG? I.e. take it from
# data in vault instead of making it a parameter? OR DO BOTH?

find(req) := mongodb.find(object.union(auth(vault.secret(secret_path(true))), req))

find_one(req) := mongodb.find_one(object.union(auth(vault.secret(secret_path(true))), req))

auth(vault_data) := {
	"uri": uri,
	"auth": auth,
} if {
	user := vault_data.user
	pass := vault_data.password
	auth := {"username": user, "password": pass}
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "27017")
	uri := sprintf("mongodb://%s:%s@%s:%s", [user, pass, host, port])
}
else := {
	"uri": uri,
} if {
	host := object.get(vault_data, "host", "localhost")
	port := object.get(vault_data, "port", "27017")
	uri := sprintf("mongodb://%s:%s", [host, port])
}

override.secret_path if false

secret_path(_) := override.secret_path if true
else := "secret/mongodb"
