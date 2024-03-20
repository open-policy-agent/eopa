# METADATA
# title: Redis helper methods
# description: |
#  Utility wrapper for `redis.query`.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of {addr: ..., password: ...}
#  at the key "redis" in mount_path "secret". The password may be omitted if
#  the Redis server is not configure to require a password.
package system.eopa.utils.redis.v1.vault

import rego.v1
import data.system.eopa.utils.vault.v1.env as vault

query(req) := redis.query(object.union({"auth": auth(vault.secret(secret_path(true)))}, req))

auth(vault_data) := {
	"username": username,
	"password": password,
} if {
	username := object.get(vault_data, "username", "")
	password := object.get(vault_data, "password", "")
}

override.secret_path if false

secret_path(_) := override.secret_path if true
else := "secret/redis"
