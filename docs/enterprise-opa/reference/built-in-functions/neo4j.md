---
sidebar_position: 10
sidebar_label: neo4j
title: "neo4j functions: Interacting with a Neo4J database | Enterprise OPA"
---

import FunctionErrors from "./_function-errors.md"
import Neo4JAuth from "../_neo4j/_neo4j-auth.md"

The `neo4j` built-in functions allow you to interact with a Neo4J database.

:::info
Check out our [tutorial](/enterprise-opa/tutorials/using-data/querying-neo4j) on querying Neo4J.
:::


### Auth

<Neo4JAuth />

<FunctionErrors />


## `neo4j.query`

The `neo4j.query` function allows you to make a query against a Neo4J database, returning multiple objects.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `uri` | String | Yes |  | The URI of the database.  |
| `auth` | Object | No |  | See [Auth](#auth) |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `raise_error` | Bool | No | true | See [Errors](#errors) |
| `query` | String | Yes | | [Cypher](https://neo4j.com/docs/getting-started/cypher-intro/) query to run against the Neo4J database. |
| `parameters` | Object | No | | Parameters for substitution into the query. |


### Example usage

```rego
neo4j.query({
  "auth": {
    "scheme": "basic",
    "principal": "neo4j",
    "credentials": "letmein1!",
  },
  "uri": "http://localhost:7687",
  "query": "MATCH (n:Pet) WHERE n.age > $a RETURN n.name",
  "parameters": {"a": 3}
}) # => { "results": [ <object>, ... ] }
```


## Utility helpers

Enterprise OPA comes with a helper method for using this builtin together with
[`vault.send`](vault): `neo4j.query`.

This method is available in Enterprise OPA at `data.system.eopa.utils.neo4j.v1.vault`.

```rego
package example
import data.system.eopa.utils.neo4j.v1.vault as neo4j

example_1 := neo4j.query({"query": " ... ", "parameters": { ... }})
# => {"results": [ ... ]}
```

The utility method will lookup connection data from a map it expects to find in
Vault, under the path `secret/neo4j`:

```rego
{
  "uri": "...",
  "scheme": "...",
  "credentials": "...",
  "principal": "...",
  "realm": "...",
}
```

If `uri` is not defined, it defaults to `neo4j://localhost:7687`. The `scheme`, `credentials`, `principal`, and `realm` keys behave as in the `auth` field of the `neo4j.query()` request object, see [Auth](#auth).

To override the secret path within Vault, use:

```rego
package example
import data.system.eopa.utils.neo4j.v1.vault as neo4j

neo4j_query(req) := result {
  result := neo4j.query(req)
    with neo4j.override.secret_path as "secret/prod/eopa-neo4j"

example_2 := neo4j_query({"query": " ... ", "parameters": { ... }})
) # => {"results": [ ... ]}
```

If you need to override the Vault address or token, you can use this:

```rego
package example
import data.system.eopa.utils.vault.v1.env as vault
import data.system.eopa.utils.neo4j.v1.vault as neo4j

neo4j_query(req) := result {
  result := neo4j.query(req)
    with neo4j.override.secret_path as "secret/prod/eopa-neo4j"
    with vault.override.address as "localhost"
    with vault.override.token as "dev-token-2"
}

example_3 := neo4j_query({"query": " ... ", "parameters": { ... }})
) # => {"results": [ ... ]}
```
