---
sidebar_position: 10
sidebar_label: dynamodb
title: "dynamodb: Interacting with a DynamoDB database | EOPA"
---

import FunctionErrors from "./_function-errors.md"

The `dynamodb` built-in functions allow you to interact with a DynamoDB database.


## Shared Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `endpoint` | String | No | [DynamoDB region-specific endpoint](https://docs.aws.amazon.com/general/latest/gr/ddb.html) | The endpoint of the DynamoDB service.  |
| `region` | String | Yes |  | The AWS region of the DynamoDB service.  |
| `credentials` | Object | No |  | See [Credentials](#credentials). |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `raise_error` | Bool | No | true | See [Errors](#errors) |


### Credentials

Credentials for AWS calls are checked in the following order:
1. Statically provided credentials (see below)
2. Standard AWS environment variables (see below)
3. ECS/EC2 role provider

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `credentials.access_key` | String | No | `AWS_ACCESS_KEY_ID` environment variable | AWS Access key ID. If provided in Rego, must also provided `credentials.secret_key` |
| `credentials.secret_key` | String | No | `AWS_SECRET_ACCESS_KEY` environment variable | AWS Secret Access Key. If provided in Rego, must also provide `credentials.access_key` |
| `credentials.session_token` | String | No | `AWS_SESSION_TOKEN` environment variable | AWS Session Token. |

<FunctionErrors />


## `dynamodb.get`

`dynamodb.get` executes a get against a DynamoDB database, returning a single `row`.


### Example usage

```rego
thread := dynamodb.get({
  "endpoint": "dynamodb.us-west-2.amazonaws.com",
  "region": "us-west-2",
  "table": "thread",
  "key": {
      "ForumName": {"S": "help"},
      "Subject": {"S": "How to write rego"}
  }
}) # => { "row": ...}
```


### Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `table` | String | Yes |  | Name of the table to query. |
| `key` | Object | Yes |  | Complex object representing the key to retrieve from the table. See [AWS example tables and data](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/AppendixSampleTables.html). |
| `consistent_read` | Bool | No | false | If true, a strongly consistent read is used; if false, an eventually consistent read is used.|


## `dynamodb.query`

`dynamodb.query` makes a query against a DynamoDB database, returning multiple `rows`.


### Example usage

See the [DynamoDB getting started guide](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/getting-started-step-5.html) for details on the data.

```rego
music := dynamodb.query({
  "region": "us-west-1",
  "table": "foo",
  "key_condition_expression": "#music = :name",
  "expression_attribute_values": {":name": {"S": "Acme Band"}},
  "expression_attribute_names": {"#music": "Artist"}
}) # => { "rows": ... }
```


### Parameters

See the [AWS SDK](https://docs.aws.amazon.com/sdk-for-go/api/service/dynamodb/#QueryInput) for information about each individual parameter.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `table` | String | Yes |  | Name of the table to query. |
| `consistent_read` | Bool | No | false | If true, a strongly consistent read is used; if false, an eventually consistent read is used.|
| `key_condition_expression` | String | Yes | | |
| `expression_attribute_names` | Object | No | | |
| `expression_attribute_values` | Object | No | | |
| `exclusive_start_key` | Object | No | | |
| `index_name` | String | No | "" | |
| `limit` | Integer | No | 0 | |
| `projection_expression` | String | No | "" | |
| `scan_index_forward` | Boolean | No | true | |
| `select` | String | No | "" | |


## Utility helpers

EOPA comes with helper methods for using these built-ins together with
[`vault.send`](vault): `dynamodb.get` and `dynamodb.query`.

Both of these methods are available in EOPA at `data.system.eopa.utils.dynamodb.v1.vault`.

```rego
package example
import data.system.eopa.utils.dynamodb.v1.vault as dynamodb

example_1 := dynamodb.get({"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}})
) # => {"row": { ... }}

example_2 := dynamodb.query({"table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})
) # => {"rows": [ ... ]}
```

The utility methods will lookup connection data from a map it expects to find in Vault,
under the path `secret/dynamodb`:

```rego
{
  "region": "...",
  "endpoint": "...",
  "access_key": "...",
  "secret_key": "...",
}
```

Of those, only `region` is mandatory, see the parameter docs above.

To override the secret path within Vault, use:

```rego
package example
import data.system.eopa.utils.dynamodb.v1.vault as dynamodb

dynamodb_query(req) := result {
  result := dynamodb.query(req)
    with dynamodb.override.secret_path as "secret/prod/eopa-dynamodb"

example_3 := dynamodb_query({"table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})
) # => {"rows": [ ... ]}
```

Override for a `dynamodb.get` is similar. If you need to override the Vault address or token, you can use this:

```rego
package example
import data.system.eopa.utils.vault.v1.env as vault
import data.system.eopa.utils.dynamodb.v1.vault as dynamodb

dynamodb_query(req) := result {
  result := dynamodb.query(req)
    with dynamodb.override.secret_path as "secret/prod/eopa-dynamodb"
    with vault.override.address as "localhost"
    with vault.override.token as "dev-token-2"

example_4 := dynamodb_query({"table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}})
) # => {"rows": [ ... ]}
```

To control the caching and error raising behavior, `cache`, `cache_duration`, and
`raise_error` can also be passed to `dynamodb.get` and `dynamodb.query` as keys of their request
objects.
