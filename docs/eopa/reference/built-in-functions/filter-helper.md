---
sidebar_position: 13
sidebar_label: filter.helper
title: "filter.helper: Testing data filtering"
---


EOPA provides a high-level `filter.helper()` built-in function for example-based testing of data filter policies. It can be used by importing

```rego
import data.system.eopa.utils.tests.v1.filter
```

See the [explanation on testing data filtering](/apps/data/explanation/testing) for more usage information.


## `filter.helper`


### Example Usage

```rego
package filters

import data.system.eopa.utils.tests.v1.filter

fruits_table := [
	{"id": 0, "name": "apple", "owner_id": "a"},
	{"id": 1, "name": "banana", "owner_id": "a"},
	{"id": 2, "name": "cherry", "owner_id": "b"},
]

users_table := [
	{"id": "a", "name": "jane"},
	{"id": "b", "name": "john"},
]

test_owner_can_see_their_fruit if {
	filtered := filter.helper(
		"data.filters.include",
		"SELECT fruits.name, users.name as owner FROM fruits LEFT JOIN users ON fruits.owner_id = users.id",
		{
			"fruits": fruits_table,
			"users": users_table,
		},
		{
			"debug": true
		}
	) with input.username as "jane"
	count(filtered) == 2
	{"name": "apple", "owner": "jane"} in filtered
	{"name": "banana", "owner": "jane"} in filtered
}
```


### Arguments

It takes four arguments:

Position | Parameter | Description | Example
---|---|---|---
1 | `filter_rule` | The data reference of the filter rule. | `data.filters.include`
2 | `sql_query` | The query to which filters are to be appended. | `SELECT fruits.name, users.name as owner FROM fruits LEFT JOIN users ON fruits.owner_id = users.id`
3 | `tables` | An object of one or more tables, `{"table_name": [<row objects>]}`| `{"fruits": [{"name": "banana"}, {"name": "cherry"}]}`
4 | `opts` | An object of extra options, see below. |

`opts` supports the following parameters:

Parameter | Type | Description | Required (default) | Example
---|---|---|---|---
`debug` | Bool | Enable printing debug output. Only visible in `eopa test -v` or on test failure. | No (`false`) | `{"debug": true}`
`db` | String | File name of SQLite3 database to write to. | No (`:memory:`) | `{"db": "temp.sqlite3"}`
`mappings` | Object | A mappings object. See [`rego.compile()`](./rego-compile#mappings) for details. | No (`{}`)| `{"fruits": {"$self": "f", "name": "name_col"}}`

:::warning
When a custom database file name is provided like this, the helper will **not** drop tables at the end of a test run,
allowing you to inspect the intermediate state of your database after `eopa test` has run.

However, this means that a subsequent run will not start with a fresh database.
It is up to the user to **remove the temporary database file between runs**.
:::


#### Outputs

`filter.helper()` returns an array of row objects corresponding to the result of the `sql_query` combined with the generated filters from your data filter policy.
For example:

```json
[
  {
    "name": "apple",
    "owner": "jane"
  },
  {
    "name": "banana",
    "owner": "jane"
  }
]
```
