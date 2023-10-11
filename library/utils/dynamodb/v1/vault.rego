# METADATA
# title: DynamoDB helper methods
# description: |
#  Utility wrapper for dynamodb.get and dynamodb.query.
#  Addresses and credentials are taken from vault, where it expects
#  to find a map of { region: ..., endpoint: ..., access_key: ... }
#  at the key "dynamodb" in mount_path "secret"
package system.eopa.utils.dynamodb.v1.vault

import future.keywords.if
import data.system.eopa.utils.vault.v1.env as vault

get(req) := dynamodb.get(object.union(auth(vault.secret(secret_path(true))), req))
query(req) := dynamodb.query(object.union(auth(vault.secret(secret_path(true))), req))

auth(vault_data) := {
	"credentials": credentials,
	"endpoint": endpoint,
	"region": vault_data.region, # required key, no default
} if {
	access_key := vault_data.access_key
	secret_key := vault_data.secret_key
	credentials := {"access_key": access_key, "secret_key": secret_key}
	endpoint := object.get(vault_data, "endpoint", "")
}
else := {
	"region": vault_data.region, # required key, no default
}

override.secret_path if false

secret_path(_) = override.secret_path if true
else := "secret/dynamodb"
