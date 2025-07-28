---
sidebar_position: 1
sidebar_label: Preview API
title: Preview API | Enterprise OPA
---

# Preview API

The preview API exposes endpoints for trying out new or updated policies and data with or without existing production policies and data. Evaluations using this API do not affect policies and data being used in the production path. This API can be disabled [with configuration](/enterprise-opa/reference/configuration/preview) if desired.


## Preview a Decision

```http
POST /v0/preview/{path:.+}
Content-Type: application/json
```

This preview endpoint behaves similar to OPA's [Data API – Get a Document (with Input)](https://www.openpolicyagent.org/docs/rest-api/#get-a-document-with-input).

The path separator is used to access values inside object and array documents. If the path indexes into an array, the server will attempt to convert the array index to an integer. If the path element cannot be converted to an integer, the server will respond with 404.


### Request Body

Request bodies are optional for the preview API. A request with no body behaves the same as the [OPA Data API – Get a Document](https://www.openpolicyagent.org/docs/rest-api/#get-a-document) endpoint. When supplied, the request body is an object that supports various keys. **All keys are optional.**

| Key | Type | Description |
| --- | --- | --- |
| input | `object[string:any]` | This is the input object to use when evaluating a decision |
| data | `object[string:any]` | This set of data is loaded in to Enterprise OPA for the evaluation. If data is included that conflicts with any existing data, the updated data is used instead of the existing data for the request. Data sent with requests is not persisted. |
| rego_modules | `object[string:string]` | This object is a set of file paths to Rego code strings. These policies are parsed and evaluated along with the request. If a policy is included that conflicts with an existing policy, the updated policy is used instead of the existing policy for the request. Policies sent with requests are not persisted. |
| rego | `string` | When provided, this string of Rego code is evaluated in the context of the request path |
| nd_builtin_cache | `object[string:object[string:any]]` | Providing this cache value allows overriding the return of built-in functions where the first key is the function name, the second key is the JSON stringified arguments to the function, and the value is the return of the built-in function call. |


### Query Parameters

While it’s unconventional for a POST request to allow query parameters, the preview API offers various settings controllable through request query parameters. **All supported parameters are booleans and default to `false`.** Some of these parameters come from the Open Policy Agent Data API, while others are custom.


#### Open Policy Agent parameters

| Parameter | Description |
| --- | --- |
| pretty | Format the JSON return with whitespace. |
| provenance | Include provenance data in the return object. |
| metrics | Include basic metrics data in the return object. |
| instrument | Include extended metrics in the return object for more in depth debugging (overrides ‘metrics’). |
| strict-builtin-errors | Return an error in the event a built-in function errors rather than evaluating to `undefined`. |


#### Custom parameters

| Parameter | Description |
| --- | --- |
| print | Include output generated with the `print()` function in the return object. |
| strict | Compile previewed rego modules in strict mode. |
| sandbox | Exclude existing policies and data when evaluating a preview request (use only what is sent). |


### Request Headers

| Name | Required | Accepted Values | Description |
| --- | --- | --- | --- |
| Content-Type | Yes | `application/json`<br /><br />`application/x-yaml` | Indicates the request body is either a JSON or YAML encoded document. |
| Content-Encoding | No | gzip | Indicates the request body is a compressed gzip object. |


### Status Codes

| Code | Reason |
| --- | --- |
| 200 | no error |
| 400 | bad request |
| 500 | server error |

The server returns 400 if the input document is invalid (i.e. malformed JSON).

The server returns 200 if the path refers to an undefined document. In this
case, the response will not contain a `result` property.


### Response Message

The server will respond with a JSON object.

| Key | Description |
| --- | --- |
| result | The base or virtual document referred to by the URL path. If the path is undefined, this key will be omitted. |
| metrics | If query metrics are enabled, this field contains query performance metrics collected during the parse, compile, and evaluation steps. |
| print | If print support is enabled, this field contains all output from `print()` statements in a new-line separated string. |
| provenance | If provenance support is enabled, this field contains information about the Enterprise OPA instance, as well as any bundles that have been loaded. |


### Examples


#### Request with an empty body

The example assumes the following policy:

```rego
package opa.examples

import rego.v1

import input.example.flag

allow_request if flag == true
```


##### Request

```http
POST /v0/preview/example?pretty HTTP/1.1
Content-Type: application/json
```


##### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
	"result": true
}
```


#### Simple package request


##### Request

```http
POST /v0/preview/example?pretty HTTP/1.1
Content-Type: application/json

{
    "input": {
        "user": "alice",
        "method": "POST",
        "path": ["dashboard", "reports","detailed"]
    },
    "data": {
        "groups": {
            "admins": ["alice"]
        }
    },
    "rego_modules": {
        "additional/helpers.rego": "package example\n\nuserInGroup(name, user) {\n   data.groups[name][_] = user\n}",
        "existing/policy.rego": "package example\n\nallow {\n  input.method == \"POST\"\n  input.path = [\"dashboard\", \"reports\", \"detailed\"]\n  print(input.user)\n  userInGroup(\"admins\", input.user)\n}\n"
    }
}
```


##### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "result": {
        "allow": true
    }
}
```


#### Package request with features


##### Request

```http
POST /v0/preview/example?pretty&print&strict&sandbox&provenance&instrument&strict-builtin-errors HTTP/1.1
Content-Type: application/json

{
    "input": {
        "user": "alice",
        "method": "POST",
        "path": ["dashboard", "reports","detailed"]
    },
    "data": {
        "groups": {
            "admins": ["alice"]
        }
    },
    "rego_modules": {
        "additional/helpers.rego": "package example\n\nuserInGroup(name, user) {\n   data.groups[name][_] = user\n}",
        "existing/policy.rego": "package example\n\nallow {\n  input.method == \"POST\"\n  input.path = [\"dashboard\", \"reports\", \"detailed\"]\n  print(input.user)\n  userInGroup(\"admins\", input.user)\n}\n"
    }
}
```


##### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "provenance": {
        "version": "0.56.0",
        "build_commit": "",
        "build_timestamp": "",
        "build_hostname": ""
    },
    "metrics": {
        "counter_compile_stage_comprehension_index_build": 0,
        "counter_regovm_eval_instructions": 63,
        "timer_compile_stage_check_duplicate_imports_ns": 3458,
        "timer_compile_stage_check_keyword_overrides_ns": 279416,
        "timer_compile_stage_check_recursion_ns": 50000,
        "timer_compile_stage_check_rule_conflicts_ns": 2114416,
        "timer_compile_stage_check_safety_rule_bodies_ns": 8881708,
        "timer_compile_stage_check_safety_rule_heads_ns": 555209,
        "timer_compile_stage_check_types_ns": 1258333,
        "timer_compile_stage_check_undefined_funcs_ns": 888875,
        "timer_compile_stage_check_void_calls_ns": 478542,
        "timer_compile_stage_init_local_var_gen_ns": 207125,
        "timer_compile_stage_parse_metadata_blocks_ns": 1107167,
        "timer_compile_stage_rebuild_comprehension_indices_ns": 262500,
        "timer_compile_stage_rebuild_indices_ns": 314917,
        "timer_compile_stage_remove_imports_ns": 1167,
        "timer_compile_stage_resolve_refs_ns": 1716792,
        "timer_compile_stage_rewrite_comprehension_terms_ns": 951917,
        "timer_compile_stage_rewrite_dynamic_terms_ns": 95584,
        "timer_compile_stage_rewrite_equals_ns": 273458,
        "timer_compile_stage_rewrite_expr_terms_ns": 2895833,
        "timer_compile_stage_rewrite_local_vars_ns": 2739875,
        "timer_compile_stage_rewrite_print_calls_ns": 1214375,
        "timer_compile_stage_rewrite_refs_in_head_ns": 263042,
        "timer_compile_stage_rewrite_rego_metadata_calls_ns": 1352209,
        "timer_compile_stage_rewrite_rule_head_refs_ns": 25959,
        "timer_compile_stage_rewrite_with_values_ns": 1041041,
        "timer_compile_stage_set_annotationset_ns": 4459,
        "timer_compile_stage_set_graph_ns": 756792,
        "timer_compile_stage_set_module_tree_ns": 19125,
        "timer_compile_stage_set_rule_tree_ns": 136708,
        "timer_compile_state_check_deprecated_builtins_ns": 183750,
        "timer_compile_state_check_unsafe_builtins_ns": 177834,
        "timer_query_compile_stage_build_comprehension_index_ns": 1791,
        "timer_query_compile_stage_check_deprecated_builtins_ns": 209,
        "timer_query_compile_stage_check_keyword_overrides_ns": 584,
        "timer_query_compile_stage_check_safety_ns": 6917,
        "timer_query_compile_stage_check_types_ns": 11500,
        "timer_query_compile_stage_check_undefined_funcs_ns": 1583,
        "timer_query_compile_stage_check_unsafe_builtins_ns": 833,
        "timer_query_compile_stage_check_void_calls_ns": 1250,
        "timer_query_compile_stage_resolve_refs_ns": 3167,
        "timer_query_compile_stage_rewrite_comprehension_terms_ns": 2875,
        "timer_query_compile_stage_rewrite_dynamic_terms_ns": 1292,
        "timer_query_compile_stage_rewrite_expr_terms_ns": 2500,
        "timer_query_compile_stage_rewrite_local_vars_ns": 8708,
        "timer_query_compile_stage_rewrite_print_calls_ns": 1417,
        "timer_query_compile_stage_rewrite_to_capture_value_ns": 9459,
        "timer_query_compile_stage_rewrite_with_values_ns": 1583,
        "timer_rego_input_parse_ns": 127458,
        "timer_rego_module_compile_ns": 30890542,
        "timer_rego_module_parse_ns": 274333,
        "timer_rego_query_compile_ns": 69042,
        "timer_rego_query_parse_ns": 60500,
        "timer_regovm_eval_ns": 69292,
        "timer_server_handler_ns": 32664750
    },
    "result": {
        "allow": true
    },
    "printed": "alice\n"
}
```


#### Simple Rego query


##### Request

```http
POST /v0/preview/example?pretty HTTP/1.1
Content-Type: application/json

{
    "input": {
        "user": "alice",
        "method": "POST",
        "path": ["dashboard", "reports","detailed"]
    },
    "rego": "input.method == \"POST\""
}
```


##### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "result": {
        "expressions": [
            {
                "value": true,
                "text": "input.method == \"POST\"",
                "location": {
                    "row": 1,
                    "col": 1
                }
            }
        ]
    }
}
```


#### Rego query using ND Builtin Cache


##### Request

```http
POST /v0/preview/example?pretty HTTP/1.1
Content-Type: application/json

{
    "rego": "http.send({\"method\": \"GET\", \"url\":\"https://example.com/todos/1\"})",
    "nd_builtin_cache": {
        "http.send": {
            "[{\"method\":\"GET\", \"url\": \"https://example.com/todos/1\"}]": {
                "body": { "response_from_nd_building_cache": true }
            }
        }
    }
}
```


##### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "result": {
        "expressions": [
            {
                "value": {
                    "body": {
                        "response_from_nd_building_cache": true
                    }
                },
                "text": "http.send({\"method\": \"GET\", \"url\":\"https://example.com/todos/1\"})",
                "location": {
                    "row": 1,
                    "col": 1
                }
            }
        ]
    }
}
```


## Authentication

If the Enterprise OPA server is configured to require authentication as described in the [OPA security documentation](https://www.openpolicyagent.org/docs/security/#authentication-and-authorization), the Preview API will also require this authentication.
