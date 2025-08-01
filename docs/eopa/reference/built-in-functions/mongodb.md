---
sidebar_position: 9
sidebar_label: mongodb
title: "mongodb functions: Interacting with a MongoDB database | EOPA"
---

import FunctionErrors from "./_function-errors.md"
import MongoDBAuth from "../_mongodb/_mongodb-auth.md"
import MongoDBFindParameters from "../_mongodb/_mongodb-find-parameters.mdx"

The `mongodb` built-in functions allow you to interact with a MongoDB database.

:::info
Check out our [tutorial](/eopa/tutorials/using-data/querying-mongodb) on querying MongoDB.

You can also use the [MongoDB data plugin](/eopa/reference/configuration/data/mongodb) to periodically push data from MongoDB directly into `data.`.
:::


## Shared Parameters

All `mongodb` functions share several function parameters.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `uri` | String | Yes |  | The URI of the database.  |
| `auth` | Object | No |  | See [Auth](#auth) |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `canonical` | Bool | No | false | Whether to use Canonical mode or not for BSON encoding. [More Details](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/) |
| `raise_error` | Bool | No | true | See [Errors](#errors) |


### Auth

<MongoDBAuth />

<FunctionErrors />


## `mongodb.find`

The `mongodb.find` function allows you to make a query against a MongoDB database, returning multiple objects.


### Example usage

```rego
allowed_resources_query_response := mongodb.find({
  "uri": "mongodb://localhost:27017",
  "database": "permissions",
  "collection": "resources",
  "filter": {
      operation_to_field[input.operation]: {
          "$in": input.user.roles
      },
  },
  "options": {"projection": {"_id": false}}
}) # => { "results": ... }
```


### Parameters

<MongoDBFindParameters options_name="options"/>


## `mongodb.find_one`

The `mongodb.find_one` function allows you to make a query against a MongoDB database, returning a single object.


### Example usage

```rego
resource_query_response := mongodb.find({
    "uri": "mongodb://localhost:27017",
    "database": "permissions",
    "collection": "resources",
    "filter": {
        "endpoint": sprintf("/%s", [concat("/", input.request.path)]),
    },
    "options": {"projection": {"_id": false}},
}) # => { "results": ... }
```


### Parameters

<MongoDBFindParameters options_name="options"/>


## Utility helpers

EOPA comes with helper methods for using this builtin together with
[`vault.send`](vault): `mongodb.find` and `mongodb.find_one`.

Both of these methods are available in EOPA at `data.system.eopa.utils.mongodb.v1.vault`.

```rego
package example
import data.system.eopa.utils.mongodb.v1.vault as mongodb

example_1 := mongodb.find({"database": "database", "collection": "collection", "filter": {}})
) # => {"results": [ ... ]}

example_2 := mongodb.find_one({"database": "database", "collection": "collection", "filter": {}})
) # => {"result": [ ... ]}
```

The utility methods will lookup connection data from a map it expects to find in Vault,
under the path `secret/mongodb`:

```rego
{
  "host": "...",
  "port": "...",
  "user": "...",
  "password": "...",
}
```

If `host` or `port` are not defined, they default to `localhost` and `27017` respectively.

To override the secret path within Vault, use:

```rego
package example
import data.system.eopa.utils.mongodb.v1.vault as mongodb

mongodb_find(req) := result {
  result := mongodb.find(req)
    with mongodb.override.secret_path as "secret/prod/eopa-mongodb"

example_3 := mongodb_find({"database": "database", "collection": "collection", "filter": {}})
) # => {"results": [ ... ]}
```

If you need to override the Vault address or token, you can use this:

```rego
package example
import data.system.eopa.utils.vault.v1.env as vault
import data.system.eopa.utils.mongodb.v1.vault as mongodb

mongodb_find(req) := result {
  result := mongodb.find(req)
    with mongodb.override.secret_path as "secret/prod/eopa-mongodb"
    with vault.override.address as "localhost"
    with vault.override.token as "dev-token-2"

example_4 := mongodb_find({"database": "database", "collection": "collection", "filter": {}})
```

The same mechanism applies to `mongodb.find_one`.

To control the caching and error raising behavior, `cache`, `cache_duration`, and
`raise_error` can also be passed to `find` and `find_one` as keys of their request
objects.
