# METADATA
# title: Neo4J helper methods
# description: |
#  Utility wrapper for `neo4j.query`.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { uri: ..., scheme: ..., principal: ..., credentials: ..., realm: ...}
#  at the key "neo4j" in mount_path "secret". Note that not all of principal,
#  credentials, realm are necessary depending on the scheme used.
package system.eopa.utils.neo4j.v1.vault

import future.keywords.if
import data.system.eopa.utils.vault.v1.env as vault

query(req) := neo4j.query(object.union(auth(vault.secret(secret_path(true))), req))

auth(vault_data) := {
	"uri": uri,
	"auth": auth,
} if {
	scheme := vault_data.scheme
	principal := object.get(vault_data, "principal", "")
	credentials := object.get(vault_data, "credentials", "")
	realm := object.get(vault_data, "realm", "")
	auth := {
		"scheme": scheme,
		"principal": principal,
		"credentials": credentials,
		"realm": realm,
	}
	uri := object.get(vault_data, "uri", "neo4j://localhost:7687")
}

override.secret_path if false

secret_path(_) := override.secret_path if true
else := "secret/neo4j"
