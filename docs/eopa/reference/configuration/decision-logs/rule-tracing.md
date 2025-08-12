---
sidebar_position: 6
sidebar_label: Rule Tracing
title: Rule Tracing in Decision Logs | EOPA
---


# Rule tracing in Decision Logs

EOPA has support for logging the intermediate results of a decision request by setting the environment variable `OPA_DECISIONS_INTERMEDIATE_RESULTS` to one of three values: `NO_VALUE`, `SHA256`, and `VALUE`.

When this is set, then EOPA's decision logs will contain an object under the key `intermediate_results`. The structure of `intermediate_keys` is:


| Configuration | Key description | Value description |
| - | - | - |
| `NO_VALUE` | Rule names | Nil |
| `SHA256` | Rule names | Array of SHA256 hashes of the computed rule evaluation results. Duplicate hashes are not included |
| `VALUE` | Rule names | Array of computed rule evaluation results. Duplicates are included. |


:::danger[Known Issue]
This feature does not currently work with EOPA's decision log plugins, but it does work with Open Policy Agent decision log configurations.

For now, the following addition into your OPA configuration file should work

```json
decision_logs:
  console: true
```
:::


## Examples

<details>
  <summary>Shared Example Rego and HTTP calls</summary>

`cats.rego`:

```rego
package example.cats

import data.example.common as ex_common

default allow = false

allow {
	ex_common.user_is_vet(input.user)
}

allow {
	ex_common.user_is_customer_for_pet(input.user, input.pet)
}
```

`common.rego`:

```rego
package example.common

import rego.v1

_user_has_any_role(user, allowedRoles) if {
	some r in user.roles
	r in allowedRoles
}

_vet_roles := ["vet", "vet_assistant", "supervet"]

user_is_vet(user) if _user_has_any_role(user, _vet_roles)

user_is_customer_for_pet(user, pet) if user.id == pet.owner

user_is_customer_for_pet(user, pet) if user.id in pet.family
```

`input.json`:

```json
{
  "input": {
    "correlationId": "0c85-9900",
    "user": {
      "id": "0077",
      "name": "Claire",
      "roles": [
        "vet"
      ]
    },
    "pet": {
      "name": "Garfield",
      "owner": "1155",
      "family": [
        "2255",
        "3355"
      ]
    }
  }
}
```

```curl
curl -d "@input.json" -X POST http://localhost:8181/v1/data/example/cats
```
</details>


### `NO_VALUE`

```json
{
  "intermediate_results": {
    "example.cats.allow": null,
    "example.common._user_has_any_role": null,
    "example.common._vet_roles": null,
    "example.common.user_is_customer_for_pet": null,
    "example.common.user_is_vet": null
  }
}
```


### `SHA256`

:::note
Undefined responses will have a fake value of `000...000`.
:::

```json
{
  "intermediate_results": {
    "example.cats.allow": [
      "9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2"
    ],
    "example.common._user_has_any_role": [
      "9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2"
    ],
    "example.common._vet_roles": [
      "468beb10a720e0ba8af357187e81f8314374756df2d5d4eda1c0c91d545ca878"
    ],
    "example.common.user_is_customer_for_pet": [
      "0000000000000000000000000000000000000000000000000000000000000000"
    ],
    "example.common.user_is_vet": [
      "9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2"
    ]
  }
}
```


### `VALUE`

```json
{
  "intermediate_results": {
    "example.cats.allow": [
      true
    ],
    "example.common._user_has_any_role": [
      true
    ],
    "example.common._vet_roles": [
      [
        "vet",
        "vet_assistant",
        "supervet"
      ]
    ],
    "example.common.user_is_customer_for_pet": [],
    "example.common.user_is_vet": [
      true
    ]
  }
}
```
