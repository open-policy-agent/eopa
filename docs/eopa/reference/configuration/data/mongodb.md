---
sidebar_position: 6
sidebar_label: MongoDB
title: MongoDB Data Plugin | EOPA
---

import MongoDBAuth from "../../_mongodb/_mongodb-auth.md"
import MongoDBFindParameters from "../../_mongodb/_mongodb-find-parameters.mdx"


# MongoDB Data Plugin

EOPA supports pulling in data from MongoDB documents using periodic polling.

:::info
You can also query MongoDB directly from a policy at query-time using the [`mongodb` built-in function](/eopa/reference/built-in-functions/mongodb).
:::


## Example Configuration

The MongoDB integration is provided through the `data` plugin, and needs to be enabled in EOPA's configuration.

```yaml
# eopa-conf.yaml
plugins:
  data:
    employees.hr:
      type: mongodb
      uri: localhost:27017
      database: permissions
      collection: employees
      filter:
        organization: HR
      polling_interval: 10s
      rego_transform: data.e2e.transform
```

With a config like this, EOPA will retrieve the documents in the employees collection in the permissions database every 10s.
The result will contain only documents containing `{organization: HR}` and the documents will be available to all policy evaluations under `data.employees.hr.{_id}`.


## Configuration

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `uri` | String | Yes |  | The URI of the database.  |
| `auth` | Object | No |  | See [Auth](#auth) |
| `canonical` | Bool | No | false | Whether to use Canonical mode or not for BSON encoding. [More Details](https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/) |
| `polling_interval` | Strong | No | 30s | The interval between polling of the database. |


### Auth

<MongoDBAuth />


### Database, Collection and Find Options

<MongoDBFindParameters options_name="find_options" />


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from MongoDB into a different format for storage in EOPA.

`rego_transform` policies take incoming messages as JSON via `input.incoming` and returns the transformed JSON.


### Example

Starting with the EOPA configuration above and the example Data

```json
[
  {
    "_id": {
      "$oid": "6520a3db73b2495b371c6eb3"
    },
    "country": "US",
    "employeeID": "1276",
    "name": "Jane Doe",
    "organization": "HR"
  },
  {
    "_id": {
      "$oid": "652715d4da89b61eaca5cab9"
    },
    "country": "US",
    "employeeID": "1337",
    "name": "Alice Abramson",
    "organization": "Product"
  },
  {
    "_id": {
      "$oid": "65271608da89b61eaca5caba"
    },
    "country": "DE",
    "employeeID": "976",
    "name": "Bob Branson",
    "organization": "HR"
  }
]
```

Our `data.e2e.transform` policy is:

```rego
package e2e

import rego.v1

transform := {c: {e.employeeID: e.name | e := input.incoming[_]; e.country == c} | c := input.incoming[_].country}
```

Then the data retrieved by the S3 plugin would be transformed by the above into:

```json
# terminal-command
curl "${ENTERPRISE_OPA_URL}/v1/data/employees/hr?pretty"
{
  "result": {
    "DE": {
      "976": "Bob Branson"
    },
    "US": {
      "1276": "Jane Doe",
    }
  }
}
```
