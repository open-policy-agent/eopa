---
sidebar_position: 11
sidebar_label: vault
title: "vault: Interacting with HashiCorp Vault | Enterprise OPA"
---

import FunctionErrors from './_function-errors.md'

The vault functions allow you to interact with HashiCorp Vault in a more direct,
request-oriented manner than the [EKM plugin](/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault).


## `vault.send`


### Example usage

If the secret `secret/this/is/a/test` stored in vault is a single key-value pair of
`foo=bar`, then this could be queried as follows:

```rego
secret := vault.send({
  "address": "http://127.0.0.1:8200",
  "token": "devonlytoken",
  "kv2_get": {
    "mount_path": "secret",
    "path": "this/is/a/test"
  }
}) # => {"data": {"foo": "bar"}}
```


### Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `address` | String | Yes |  | Address of Vault server to send request to. |
| `token` | String | Yes |  | Token to use for  authentication. |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `raise_error` | Bool | No | true | See [Errors](#errors) |

<FunctionErrors />


### Utility methods

Enterprise OPA comes with helper methods for using this builtin, and take its configuration
from the environment variables `VAULT_ADDRESS` and `VAULT_TOKEN`: `vault.secret` and `vault.secret_opts`.

Both of these methods are available in Enterprise OPA at `data.system.eopa.utils.vault.v1.env`.

```rego
package example

import data.system.eopa.utils.vault.v1.env as vault

example_1 := vault.secret("secret/this/is/a/secret") # => {"foo": "bar"}
```

If you need to override the address or token and still want to use the convenient wrapper,
use this:

```rego
package example

import data.system.eopa.utils.vault.v1.env as vault

vault_secret(path) := result {
	result := vault.secret(path) with vault.override.address as "localhost"
		with vault.override.token as "dev-token-2"
}

example_2 := vault_secret("secret/this/is/a/secret")
```

Full control over the caching and error raising behavior is exposed via `secret_opts`:

```rego
package example

import data.system.eopa.utils.vault.v1.env as vault

example_3 := vault.secret_opts("a/b/c/d", {"cache": true, "cache_duration": "10s", "raise_error": false})
```
