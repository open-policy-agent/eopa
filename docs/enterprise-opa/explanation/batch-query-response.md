---
sidebar_position: 1
sidebar_label: Batch Query Response
title: Batch Query Response Types in Enterprise OPA
---


# Batch Query Response Types in Enterprise OPA

:::danger
:::

When using the Batch Query API, it is possible to get multiple return value types across your possible inputs.

For example, using the following rego:

```rego
package authz

import rego.v1

default allow := false

allow := "Go ask the white rabbit!" if input.user.name == "alice"

allow if input.user.role == "admin"
```

Then the following HTTP request will result in the following HTTP response:

```http
// Request
POST /v1/batch/data/authz/allow
Content-Type: application/json

{
  "inputs": {
    "A": {
      "user": {
        "name": "alice"
      }
    },
    "B": {
      "user": {
        "name": "bob",
        "role": "admin"
      }
    }
  },
  "common_input": {
    "action": "read",
    "object": "id1234",
    "user": {
        "role": "viewer"
    }
  }
}
```


```http
// Response
HTTP/1.1 200 OK
Content-Type: application/json

{
    "batch_decision_id": "6a0eb96f-db40-4cd5-8ae1-b67139721917",
    "responses": {
        "A": {
            "decision_id": "cc500f92-f907-4e4f-aa57-e0f97f9169f8",
            "result": "Go ask the white rabbit!"
        },
        "B": {
            "decision_id": "9421c856-4c32-4e95-b77a-14567ebe43ee",
            "result": true
        }
    }
}
```

The resulting value could be a string or a boolean.

Writing your policy in such a manner is _strongly advised against_.
