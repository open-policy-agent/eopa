---
sidebar_position: 1
sidebar_label: Querying SQL
title: Querying SQL with sql.send | EOPA
---

# Querying SQL with `sql.send`

EOPA has a [built-in function](/eopa/reference/built-in-functions/sql), `sql.send` for directly querying SQL databases.
This enables writing policies that pull information directly from existing database records.


## Overview

In this tutorial we'll be walking through an attribute-based access control (ABAC) example similar to the [HTTP API Authorization example][opa-http-authz-example] from the OPA documentation, but with a distinct twist: all of our policy's data about the company employees and management structure will be stored in an external database!


## Project setup

For this tutorial, we'll be using the following `docker-compose.yaml` file:

```yaml
# docker-compose.yaml
version: '3'
services:
  api_server:
    image: openpolicyagent/demo-restful-api:0.2
    ports:
      - "5000:5000"
    environment:
      - OPA_ADDR=http://eopa:8181
      - POLICY_PATH=/v1/data/httpapi/authz
    depends_on:
      - eopa
  eopa:
    image: ghcr.io/open-policy-agent/eopa:latest
    expose:
      - "8181"
    ports:
      - "8181:8181"
    volumes:
      - "./:/data"
    command:
      - "run"
      - "--server"
      - "--log-level=debug"
      - "--log-format=json-pretty"
      - "--set=decision_logs.console=true"
      - "--addr=0.0.0.0:8181"
      - "/data/example.rego"
```


## Creating our database

EOPA currently supports [Microsoft SQL Server][sqlserver], [MySQL-compatible][mysql], [PostgreSQL-compatible][postgres], [Snowflake][snowflake], and [SQLite][sqlite] databases, so we will use a small SQLite database for this tutorial to illustrate what is possible.

In a file name `init-database.sql`, insert the following SQL schema code and DDL statements:

```sql
-- Create tables for EOPA to query.
CREATE TABLE employees (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    salary INTEGER NOT NULL
);

CREATE TABLE subordinates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    manager TEXT NOT NULL,
    subordinate TEXT NOT NULL
);

-- Populate employees table.
INSERT INTO "employees" ("name", "salary") VALUES ('alice', 65000);
INSERT INTO "employees" ("name", "salary") VALUES ('bob', 90000);
INSERT INTO "employees" ("name", "salary") VALUES ('betty', 80000);
INSERT INTO "employees" ("name", "salary") VALUES ('charlie', 60000);

-- Populate subordinates table.
INSERT INTO "subordinates" ("manager", "subordinate") VALUES ('bob', 'alice');
INSERT INTO "subordinates" ("manager", "subordinate") VALUES ('betty', 'charlie');
```

We can then create our database with the following `sqlite3` CLI command:

```bash
# terminal-command
sqlite3 company.db < init-database.sql
```


## Creating the Policy

In the original example, the Rego policy directly encodes information about the company hierarchy, like so:

```rego
# bob is alice's manager, and betty is charlie's.
subordinates = {"alice": [], "charlie": [], "bob": ["alice"], "betty": ["charlie"]}
```

Since we're pulling all of our information from the database now, we can replace the spot where the `subordinates` variable was used in the original policy with a concise, [parameterized SQL query][prepared-stmt]:

```rego
# Allow managers to get their subordinates' salaries.
allow {
	some username
	input.method == "GET"
	input.path = ["finance", "salary", username]
	subordinate := sql.send({
		"driver": "sqlite",
		"data_source_name": "/data/company.db",
		"query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
		"args": [input.user, username],
	})
	count(subordinate.rows) > 0 # Make sure the row exists in the subordinates table.
}
```

:::danger
To avoid [SQL Injection Attacks][sql-injection], we recommend using prepared statements/parametrized queries, as shown in the example above.
This greatly limits the potential damage from malicious inputs, and prevents unintended queries from being run against your database.

The syntax of parameterized queries depends on the type of database,
e.g.  with PostgreSQL and SQLite, you can refer to parameters by
position, using `$1`, `$2`, etc. With MySQL, you should use `?`
instead. With Microsoft SQL Server, you should use `@p1`, `@p2`,
etc. Consult the documentation of your specific database to learn the
appropriate syntax.
:::

   [sql-injection]: https://en.wikipedia.org/wiki/SQL_injection

With that in mind, here's what our `example.rego` file should look like:

```rego
package httpapi.authz

# HTTP API request
# input = {
#   "path": ["finance", "salary", "alice"],
#   "user": "alice",
#   "method": "GET"
# }

default allow = false

# Allow users to get their own salaries.
allow {
	some username
	input.method == "GET"
	input.path = ["finance", "salary", username]
	input.user == username
}

# Allow managers to get their subordinates' salaries.
allow {
	some username
	input.method == "GET"
	input.path = ["finance", "salary", username]
	subordinate := sql.send({
		"driver": "sqlite",
		"data_source_name": "/data/company.db",
		"query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
		"args": [input.user, username],
	})
	count(subordinate.rows) > 0 # Make sure the row exists in the subordinates table.
}
```

The first `allow` rule in the policy ensures that employees can see their own salaries.
The second rule is the more interesting one, allowing the manager of an employee to see the employee's salary.
We have a table in the database encoding these relationships, so we only need to do an SQL query with the `manager` and `subordinate` fields filled in appropriately for the request, and then we can check to see if the database returned any results.


## Running the demo

To show off this example, we will use the `docker-compose.yaml` file we wrote earlier to bring up two containers: one for the demo REST server, and one for EOPA.

To start the containers, run `docker compose up`.

Once the containers have started, we can run the same queries as in the original demo using the following `curl` script:

```bash
# Check that `alice` can see her own salary
# terminal-command
curl --user alice:password localhost:5000/finance/salary/alice

# Check that `bob` can see `alice`’s salary (because `bob` is `alice`’s manager.)
# terminal-command
curl --user bob:password localhost:5000/finance/salary/alice

# Check that `bob` cannot see `charlie`'s salary
# terminal-command
curl --user bob:password localhost:5000/finance/salary/charlie
```

We should see the following results, indicating that everything is working as expected:

```txt
Success: user alice is authorized
Success: user bob is authorized
Error: user bob is not authorized to GET url /finance/salary/charlie
```

   [opa-http-authz-example]: https://www.openpolicyagent.org/docs/http-api-authorization/
   [mysql]: https://www.mysql.com/
   [postgres]: https://www.postgresql.org/
   [snowflake]: https://www.snowflake.com
   [sqlite]: https://www.sqlite.org/index.html
   [sqlserver]: https://www.microsoft.com/en-us/sql-server
   [sql-injection]: https://en.wikipedia.org/wiki/SQL_injection
   [prepared-stmt]: https://en.wikipedia.org/wiki/Prepared_statement
