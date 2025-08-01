---
sidebar_position: 8
sidebar_label: sql
title: "sql: Interacting with a SQL database | EOPA"
---

import FunctionErrors from './_function-errors.md'

The sql functions allow you to interact with many SQL databases,
including any MySQL-compatible or PostgreSQL-compatible database.

These include:
- CockroachDB
- MariaDB
- Microsoft SQL Server
- MySQL
- Oracle
- PlanetScale
- PostgreSQL
- SQLite
- SingleStore (MemSQL)
- Snowflake
- TiDB

:::info
Check out our [tutorial](/enterprise-opa/tutorials/using-data/querying-sql) on implementing ABAC using SQL.
:::


## Shared Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `driver` | String | Yes |  | `mysql`, `postgres`, `sqlite`, `snowflake`, `sqlserver`. The driver name of the queried database.|
| `data_source_name` | String | Yes |  | See [Data Source Name](#data-source-name) |
| `max_open_connections` | Integer | No | 0 | Maximum open connections to the database. |
| `max_idle_connections` | Integer | No | 2 | Maximum idle connections to the database. |
| `connection_max_idle_time` | Integer | No | 0 (Indefinite) | Maximum idle time (in seconds) of each connection. |
| `connection_max_life_time` | Integer | No | 0 (Indefinite) | Maximum life time (in seconds) of each connection. |
| `max_prepared_statements` | Integer | No | 128 | Maximum number of prepared statements allowed in a query.  |
| `cache` | Bool | No | false | Cache the results of queries. |
| `cache_duration` | Integer | No | 60 | Duration (in seconds) to keep cached query results. |
| `raise_error` | Bool | No | true | See [Errors](#errors) |


### Data Source Name

The `data_source_name` parameter is a string containing database connection information.
Data source names (also called connection strings) often share a similar format:

```raw
scheme://username:password@host:port/dbname?param1=value1&param2=value2&...
```

where `scheme` corresponds to the database driver, e.g.: `postgres`, `sqlserver`, or `oracle`.


#### Snowflake

Snowflake's `data_source_name` must omit the scheme, for example:

```raw
user:password@my_org-my_account/mydb/myschema?warehouse=mywh
```

<!-- NOTE(sr): when https://github.com/StyraInc/enterprise-opa-private/pull/2094 is merged and released,
this restriction can be removed.-->


#### MySQL

MySQL is using its own variation of the URI format:

```raw
[username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
```

For example:

```raw
user:password@tcp(localhost:32854)/mydb?tls=skip-verify
```


#### PostgreSQL

For PostgreSQL, you can use the `key=value` format alternatively to the URL format:

```raw
host=localhost port=5432 dbname=mydb user=username password=password
```


### References

Check the documentation of the database type for the authoritative source on the format of the connection string, e.g.:
- [Microsoft SQL Server](https://github.com/microsoft/go-mssqldb#connection-parameters-and-dsn)
- [MySQL 8.1](https://dev.mysql.com/doc/refman/8.1/en/connecting-using-uri-or-key-value-pairs.html)
- [PostgreSQL](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING)
- [Snowflake](https://pkg.go.dev/github.com/snowflakedb/gosnowflake#hdr-Connection_String)

<FunctionErrors />


## `sql.send`


### Example usage

```rego
subordinate := sql.send({
  "driver": "sqlite",
  "data_source_name": "/data/company.db",
  "query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
  "args": [input.user, username],
})
```


### Parameters

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `query` | String | Yes |  | Query to execute against the database. |
| `args` | String | No |  | Arguments supplied to the query. |
| `row_object` | Bool | No | false | When set to `true`, the results of the query will be transformed into an object with column names as keys. |


### Example Response

Given the following table schema and values

```sql
CREATE TABLE T1 (id TEXT, description TEXT);
INSERT INTO T1(id, description) VALUES('A', 'B');
```

If `row_object` is false

```rego
{
  "rows": [["A", "B"]]
}
```

If `row_object` is `true`

```rego
{
  "rows": [{"id": "A", "description": "B"}]
}
```


### Utility methods

EOPA comes with helper methods for using this builtin together with
[`vault.send`](vault):

1. `mysql.send` and `mysql.send_opts`
2. `postgres.send` and `postgres.send_opts`

All of these methods are available in EOPA at `data.system.eopa.utils.mysql.v1.vault`
and `data.system.eopa.utils.postgres.v1.vault` respectively.

```rego
package example

import data.system.eopa.utils.mysql.v1.vault as mysql
import data.system.eopa.utils.postgres.v1.vault as postgres

example_1 := mysql.send("SELECT * FROM users WHERE id = ?", [input.id])

# => {"rows": [ ... ]}

example_2 := postgres.send("SELECT * FROM users WHERE id = $1", [input.id])

# => {"rows": [ ... ]}
```

The utility methods will lookup connection data from a map it expects to find in Vault,
under the path `secret/mysql` and `secret/postgres`:

```rego
{
  "host": "...",
  "port": "...",
  "user": "...",
  "password": "...",
}
```

For `postgres.send`, the TLS verification is configured by the key `sslmode`
(defaults to `require`), for `mysql.send` it's `tls` (`true`).

If `host` or `port` are not defined, they default to `localhost` and port `3306`
(MySQL), and `5432` (PostgreSQL).

To override the secret path within Vault, use:

```rego
package example
import data.system.eopa.utils.mysql.v1.vault as mysql

mysql_send(query, args) := result {
  result := mysql.send(query, args)
    with mysql.override.secret_path as "secret/prod/eopa-mysql"

example_3 := mysql_send("SELECT * FROM users WHERE id = ?", [input.x])
) # => {"rows": [ ... ]}
```

If you need to override the Vault address or token, you can use this:

```rego
package example
import data.system.eopa.utils.vault.v1.env as vault
import data.system.eopa.utils.mysql.v1.vault as mysql

mysql_send(query, args) := result {
  result := mysql.send(query, args)
    with mysql.override.secret_path as "secret/prod/eopa-mysql"
    with vault.override.address as "localhost"
    with vault.override.token as "dev-token-2"

example_4 := mysql_send("SELECT * FROM users WHERE id = ?", [input.x])
) # => {"rows": [ ... ]}
```

The same mechanism applies to `postgres.send`.

To control the caching and error raising behavior, `cache`, `cache_duration`,
`raise_error`, and all other config keys can be passed to `mysql.send_opts` and
`postgres.send_opts` as a third object argument:

```rego
package example
import data.system.eopa.utils.mysql.v1.vault as mysql

example_3 := mysql.send_opts("SELECT * FROM users WHERE id = ?", [input.x], {"cache": true, "cache_duration": "30s", "raise_error": false})
) # => {"rows": [ ... ]}
```
