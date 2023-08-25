# METADATA
# title: Vault helper method
# description: |
#  Utility wrapper for vault.send, taking address and token from
#  env vars VAULT_ADDRESS and VAULT_TOKEN, respectively.
package system.eopa.utils.vault.v1.env

import future.keywords.if

secret(path) := secret_opts(path, {})

secret_opts(path, opts) := vault.send(object.union(
        {
                "address": address(true),
                "token": token(true),
                "kv2_get": req,
        },
        opts,
)).data if {
	xs := split(path, "/")
	mount := xs[0]
	_path := concat("/", array.slice(xs, 1, count(xs)))
	req := {"mount_path": mount, "path": _path}
}

override.address if false
override.token if false

address(_) := override.address if true
else := opa.runtime().env.VAULT_ADDRESS

token(_) := override.token if true
else := opa.runtime().env.VAULT_TOKEN
