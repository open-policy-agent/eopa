---
sidebar_position: 11
sidebar_label: redis
title: "redis: Interacting with a Redis database | Enterprise OPA"
---

import FunctionErrors from "./_function-errors.md"
import RedisAuth from "../_redis/_redis-auth.md"

The `redis` built-in function allow you to interact with a Redis database.

:::info
Check out our [tutorial](/enterprise-opa/tutorials/using-data/querying-redis) on querying Redis.
:::


## `redis.query`

The `redis.query` function allows you to make a query against a Redis database.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `addr` | String | Yes |  | Address to connect to Redis at. |
| `db` | Int | No | 0 | Redis database to use. |
| `auth` | Object | No |  | See [Auth](#auth) |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `raise_error` | Bool | No | true | See [Errors](#errors). |
| `command` | String | Yes | | Redis [command](https://redis.io/commands/) to execute. This field is case-insensitive. |
| `args` | Array | Yes | | Arguments to pass to the Redis command. |

Note that only the following Redis commands are supported. Using a Redis command not in the list below as the value for the `command` field will cause Enterprise OPA to exit with an error.

- [get](https://redis.io/commands/get)
- [getrange](https://redis.io/commands/getrange)
- [hexists](https://redis.io/commands/hexists)
- [hget](https://redis.io/commands/hget)
- [hgetall](https://redis.io/commands/hgetall)
- [hkeys](https://redis.io/commands/hkeys)
- [hlen](https://redis.io/commands/hlen)
- [hmget](https://redis.io/commands/hmget)
- [hrandfield](https://redis.io/commands/hrandfield)
- [lindex](https://redis.io/commands/lindex)
- [llen](https://redis.io/commands/llen)
- [lpos](https://redis.io/commands/lpos)
- [lrange](https://redis.io/commands/lrange)
- [mget](https://redis.io/commands/mget)
- [scard](https://redis.io/commands/scard)
- [sdiff](https://redis.io/commands/sdiff)
- [sinter](https://redis.io/commands/sinter)
- [sintercard](https://redis.io/commands/sintercard)
- [sismember](https://redis.io/commands/sismember)
- [smembers](https://redis.io/commands/smembers)
- [smismember](https://redis.io/commands/smismember)
- [srandmember](https://redis.io/commands/srandmember)
- [strlen](https://redis.io/commands/strlen)
- [sunion](https://redis.io/commands/sunion)


### Example usage

```rego
redis.query({
  "addr": "localhost:6379",
  "auth": {
    "password": "letmein1!",
  },
  "command": "get",
  "args": ["some string key"]
}) # => { "results": "<value associated with 'some string key'>" }
```


### Auth

<RedisAuth />

<FunctionErrors />


## Utility helpers

Enterprise OPA comes with a helper method for using this builtin together with
[`vault.send`](vault): `redis.query`.

This method is available in Enterprise OPA at `data.system.eopa.utils.redis.v1.vault`.

```rego
package example
import data.system.eopa.utils.redis.v1.vault as redis

example_1 := redis.query({"addr": " ... ", "command": " ... ", "args": [ ... ]})
# => {"results": [ ... ]}
```

The utility method will lookup connection data from a map it expects to find in
Vault, under the path `secret/redis`:

```rego
{
  "username": "...",
  "password": "...",
}
```

See [Auth](#auth) for more information.

To override the secret path within Vault, use:

```rego
package example
import data.system.eopa.utils.redis.v1.vault as redis

redis_query(req) := result {
  result := redis.query(req)
    with redis.override.secret_path as "secret/prod/eopa-redis"

example_2 := redis_query({"addr": " ... ", "command": " ... ", "args": [ ... ]})
) # => {"results": ... }
```

If you need to override the Vault address or token, you can use this:

```rego
package example
import data.system.eopa.utils.vault.v1.env as vault
import data.system.eopa.utils.redis.v1.vault as redis

redis_query(req) := result {
  result := redis.query(req)
    with redis.override.secret_path as "secret/prod/eopa-redis"
    with vault.override.address as "localhost"
    with vault.override.token as "dev-token-2"
}

example_3 := redis_query({"addr": " ... ", "command": " ... ", "args": [ ... ]})
) # => {"results": [ ... ]}
```
