---
sidebar_position: 1
sidebar_label: Batch API
title: Batch Query API | Enterprise OPA
---

# Batch Query API

The batch query API exposes an endpoint for executing multiple policy evaluations in a single request.

```http
POST /v1/batch/data/{path:.+}
Content-Type: application/json
```

This batch query endpoint behaves similar to OPA's [Data API – Get a Document (with Input)](https://www.openpolicyagent.org/docs/rest-api/#get-a-document-with-input).


## Request Body

| Key | Type | Description |
| --- | --- | --- |
| `inputs` | `object[string:any]` | The inputs object is a map of the inputs for each individual query in the batch. The keys are arbitrary IDs representing each request, and the value is the JSON input object for that request |
| `common_input` | `object[string:any]` | Shared input object which individual inputs are deep merged with. Any keys specified in both `common_input` and an individual input in `inputs` will prefer the `inputs` value.|


## Query Parameters

**All supported parameters are booleans and default to `false`.** These parameters come from the Open Policy Agent Data API.


### Open Policy Agent parameters

| Parameter | Description |
| --- | --- |
| `pretty` | Format the JSON return with whitespace. |
| `provenance` | Include provenance data in the return object. |
| `metrics` | Include basic metrics data in the return object. |
| `instrument` | Include extended metrics in the return object for more in depth debugging (overrides ‘`metrics`’). |
| `strict-builtin-errors` | Return an error in the event a built-in function errors rather than evaluating to `undefined`. |


## Request Headers

| Name | Required | Accepted Values | Description |
| --- | --- | --- | --- |
| `Content-Type` | Yes | `application/json`<br /><br />`application/x-yaml` | Indicates the request body is either a JSON or YAML encoded document. |
| `Content-Encoding` | No | gzip | Indicates the request body is a compressed gzip object. |


## Status Codes

| Code | Reason |
| --- | --- |
| 200 | Each batched query returned a 200 |
| 207 | The batched queries returned a mix of other codes |
| 400 | Client side error, e.g. malformed input |
| 500 | Each batched query returned a 500 |

The server returns 400 if the input document is invalid (i.e. malformed JSON).

The server returns 200 if the path refers to an undefined document.
In this case, the response will contain an empty body.


## Response Message


### 200 Response

| Key | Type | Description |
| --- | --- | --- |
| `batch_decision_id` | `string` | If decision logging is enabled, this field contains a unique string identifier representing the entire batch of responses. |
| `metrics` | `object[string:object]` | If query metrics are enabled, this field contains query performance metrics of the batch request. |
| `responses` | `object[string:object]` | An object containing individual responses keyed with the same key as in the request. |
| `responses[_].decision_id` | `string` | If decision logging is enabled, this field contains a unique string identifier representing the individual response. |
| `responses[_].result` | `any` | The result of evaluating the policy with the given input. |
| `responses[_].metrics` | `object[string:object]` | If query metrics are enabled, this field contains query performance metrics collected during the parse, compile, and evaluation steps. |
| `responses[_].provenance` | `object[string:object]` | If provenance support is enabled, this field contains information about the Enterprise OPA instance, as well as any bundles that have been loaded. |


### 400 Response

A 400 response represents a client side error.

See [OPA documentation](https://www.openpolicyagent.org/docs/rest-api/#errors) for information about each of these keys.


| Key | Type |
| --- | --- |
| `code` | `string` |
| `message` | `string` |
| `errors` | `list[object]` |
| `errors[].code` | `string` |
| `errors[].message` | `string` |
| `errors[].location` | `object[string:string]` |
| `errors[].location.file` | `string` |
| `errors[].location.row` | `number` |
| `errors[].location.column` | `number` |


### 500 Response

A 500 response represents a server side error for all policy evaluations.

See [OPA documentation](https://www.openpolicyagent.org/docs/rest-api/#errors) for information about each of these keys.


| Key | Type | Description |
| --- | --- | --- |
| `batch_decision_id` | `string` | If decision logging is enabled, this field contains a unique string identifier representing the entire batch of responses. |
| `responses[_].decision_id` | `string` | If decision logging is enabled, this field contains a unique string identifier representing the individual response. |
| `responses[_].code` | `string` |
| `responses[_].message` | `string` |
| `responses[_].errors` | `list[object]` |
| `responses[_].errors[].code` | `string` |
| `responses[_].errors[].message` | `string` |
| `responses[_].errors[].location` | `object[string:string]` |
| `responses[_].errors[].location.file` | `string` |
| `responses[_].errors[].location.row` | `number` |
| `responses[_].errors[].location.column` | `number` |


### 207 Response

Each value in the responses field contains an HTTP status code representing the result of each query. The rest of the fields in the result correspond to the same fields as in the pure 200 or 500 cases.

| Key | Type | Description |
| --- | --- | --- |
| `responses[_].http_status_code` | `string` | This key contains the HTTP Status Code for the individual response. |
| `metrics` | `object[string:object]` | If query metrics are enabled, this field contains query performance metrics of the batch request. |


## Examples


### Successful Evaluations

Using the policy

```rego
package app.abac

import rego.v1

default allow := false

allow if input.user.title == "owner"

allow if input.user.tenure > 10
```


#### Request

```http
POST /v1/batch/data/app/abac/allow
Content-Type: application/json

{
    "inputs": {
        "1": {
            "user": {"name": "bob", "title": "owner", "tenure": 20},
            "action": "read",
            "resource": "dog123"
        },
        "2": {
            "user": {"name": "alice", "title": "manager", "tenure": 15},
            "action": "read",
            "resource": "dog123"
        },
        "3": {
            "user": {"name": "charlie", "title": "worker", "tenure": 5},
            "action": "read",
            "resource": "dog123"
        }
    }
}
```


#### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "responses": {
        "1": {
            "result": true
        },
        "2": {
            "result": true
        },
        "3": {
            "result": false
        }
    }
}
```


### Mixed Status Response with Decision Logging Enabled and Metrics

Using the policy

```rego
package app.abac

import rego.v1

default allow := false

allow if input.user.title == "owner"

allow if input.user.tenure > 10

allow := false if input.user.title == "owner"
```


#### Request


```http
POST /v1/batch/data/app/abac/allow?metrics=true
Content-Type: application/json

{
    "inputs": {
        "1": {
            "user": {"name": "bob", "title": "owner", "tenure": 20},
            "action": "read",
            "resource": "dog123"
        },
        "2": {
            "user": {"name": "alice", "title": "employee"},
            "resource": "dog123"
        }
    }
}
```


#### Response

```http
HTTP/1.1 207 Mixed-Status
Content-Type: application/json

{
    "batch_decision_id": "08bf7b03-b559-4789-bab7-3c6254fb986a",
    "metrics": {
        "counter_server_query_cache_hit": 0,
        "timer_rego_input_parse_ns": 188541,
        "timer_server_handler_ns": 2317250
    },
    "responses": {
        "1": {
            "code": "internal_error",
            "message": "eval_conflict_error: complete rules must not produce multiple outputs",
            "decision_id": "41950d4f-20ad-4439-b0cd-dcb067d1e373",
            "http_status_code": "500"
        },
        "2": {
            "decision_id": "16ba30f4-b275-47df-b92a-99e4d17cfb13",
            "metrics": {
                "counter_regovm_eval_instructions": 23,
                "counter_regovm_virtual_cache_hits": 0,
                "counter_regovm_virtual_cache_misses": 1,
                "counter_server_query_cache_hit": 1,
                "timer_regovm_eval_ns": 340500,
                "timer_server_handler_ns": 649750
            },
            "result": false,
            "http_status_code": "200"
        }
    }
}
```


### Common Inputs

```rego
package app.abac

import rego.v1

default allow := false

allow if input.user.name == "eve"

allow if input.user.role == "admin"

allow if {
	input.action == "write"
	input.user.role == "writer"
}
```


#### Request

```http
POST /v1/batch/data/app/abac/allow
Content-Type: application/json

{
  "inputs": {
    "A": {
      "user": {
        "name": "alice",
      }
      "action": "write"
    },
    "B": {
      "user": {
        "name": "bob"
        "role": "admin"
      }
    },
    "C": {
      "user": {
        "name": "eve"
      }
    }
  },
  "common_input": {
    "action": "read",
    "object": "id1234"
    "user": {
        "role": "viewer"
    }
  }
}
```


#### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "batch_decision_id": "50823120-f46b-4449-a363-6881301cff19",
    "responses": {
        "A": {
            "decision_id": "ee807043-8fe0-4edb-b2b8-edd860173a59",
            "result": false
        },
        "B": {
            "decision_id": "17b586a6-84c7-463a-955a-1784c1bfa392",
            "result": true
        },
        "C": {
            "decision_id": "2a6ed9ee-2299-4928-90b6-aac03cebdf75",
            "result": true
        }
    }
}
```
