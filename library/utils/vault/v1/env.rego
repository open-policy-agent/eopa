# METADATA
# title: Vault helper method
# description: |
#  Utility wrapper for vault.send, taking address and token from
#  env vars VAULT_ADDRESS and VAULT_TOKEN, respectively.
package system.eopa.utils.vault.v1.env

import future.keywords.if

secret(path) := secret_opts(path, {})

secret_opts(path, opts) := vault.send({
	"address": address(env),
	"token": token(env),
	"kv2_get": req,
	"cache": cache,
	"cache_duration": cache_duration,
	"raise_error": raise_error,
}).data if {
	env := opa.runtime().env
	xs := split(path, "/")
	mount := xs[0]
	_path := concat("/", array.slice(xs, 1, count(xs)))
	req := {"mount_path": mount, "path": _path}
	cache := object.get(opts, "cache", false)
	cache_duration := object.get(opts, "cache_duration", "60s")
	raise_error := object.get(opts, "raise_error", true)
}

override.address if false
override.token if false

address(env) := override.address if true
else := env.VAULT_ADDRESS

token(env) := override.token if true
else := env.VAULT_TOKEN
