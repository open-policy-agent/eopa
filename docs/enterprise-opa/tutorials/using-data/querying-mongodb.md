---
sidebar_position: 2
sidebar_label: Querying MongoDB
title: Querying MongoDB | Enterprise OPA
---

# Querying MongoDB

Enterprise OPA provides the `mongodb.find` and `mongodb.find_one` [built-in functions](/enterprise-opa/reference/built-in-functions/mongodb) for querying MongoDB at the time of a policy decision.


## Overview

In the following example, we'll be using a traditional RBAC policy to determine whether a user is allowed to access a
resource or not. Most of the data we need will be provided as part of the `input`:

- The `path` of the request
- The request `method`
- The user's `roles`

```json
// example input
{
    "request": {
        "method": "PUT",
        "path": ["finance", "reports", "q2-2021.pdf"]
    },
    "user": {
        "id": "alice",
        "roles": ["developer", "reports-reader"]
    }
}
```

What we **don't** know is whether the roles provided for a user is sufficient for access to the requested resource.
This data resides in a MongoDB database, and we'll use the `mongodb.find_one` built-in function to query it.


## Project setup

If you'd like to try the example yourself, the following steps will get you started.

1. Install `mongosh`, via `brew` or any other [available method](https://www.mongodb.com/docs/mongodb-shell/install/).

    ```shell
    # terminal-command
    brew install mongosh
    ```

2. An instance of MongoDB running, or simply use Docker to launch one:

    ```shell
    # terminal-command
    docker run -p 27017:27017 -d mongo:latest
    ```

3. Some data to query. For this example, we'll use a database called `permissions`, containing a collection of
   `resources`. This list could be made much longer, but a few items will be enough to demonstrate the built-in
   function's features. We'll use a simple [script](https://www.mongodb.com/docs/mongodb-shell/write-scripts/)
   for getting our example data into the database.

    ```javascript
    // connect-and-insert.js
    db = connect('mongodb://localhost/permissions');
    db.resources.insertMany([
        {
            endpoint: '/finance',
            allowReadRoles: ['finance-admin'],
            allowWriteRoles: ['finance-admin'],
            allowAdminRoles: ['finance-admin'],
        },
        {
            endpoint: '/finance/reports',
            allowReadRoles: ['finance-admin', 'reports-admin', 'reports-reader'],
            allowWriteRoles: ['finance-admin', 'reports-admin', 'reports-writer'],
            allowAdminRoles: ['finance-admin', 'reports-admin'],
        },
        {
            endpoint: '/finance/reports/q1-2021.pdf',
            allowReadRoles: ['finance-admin', 'reports-admin', 'reports-reader'],
            allowWriteRoles: ['finance-admin', 'reports-admin', 'reports-writer'],
            allowAdminRoles: ['finance-admin', 'reports-admin'],
        },
        {
            endpoint: '/finance/reports/q2-2021.pdf',
            allowReadRoles: ['finance-admin', 'reports-admin', 'reports-reader'],
            allowWriteRoles: ['finance-admin', 'reports-admin', 'reports-writer'],
            allowAdminRoles: ['finance-admin', 'reports-admin'],
        },
    ])
    ```
    To populate the database with the data, use `mongosh`:

    ```shell
    # terminal-command
    mongosh --file scripts/connect-and-insert.js
    ```


## Simple RBAC policy

From the `input`, we know the roles of the user and the resource they're trying to access in the form of a `path`.
Using the `path` from the `input`, we may query the database for a document where the `patch` matches the `endpoint`
field. Since we know there can only be one document matching any given `endpoint`, we'll use the `find_one` option
for our `mongodb.find_one` query:

```rego
resource_query_response := mongodb.find_one({
    "uri": "mongodb://localhost:27017",
    "database": "permissions",
    "collection": "resources",
    "filter": {
        "endpoint": sprintf("/%s", [concat("/", input.request.path)]),
    },
    "options": {"projection": {"_id": false}}
})
```

Predictably, the `uri` attribute is used to specify the location of the MongoDB instance. Both `database` and `collection` should be self-explanatory. The
[filter](https://www.mongodb.com/docs/compass/current/query/filter/) attribute determines the "question" we'll want
to ask — in this case we're only interested in matching the `endpoint` field with a path provided in the request, which
we can do with the help of `sprintf` to add a leading `/` and `concat` to join the path segments into a single string
with a `/` in between each path component. Finally, we'll use the `options` attribute to specify attributes we'd rather
not have included in the response, which in this case is just the `_id` field.

Given a `input.request.path` of `["finance", "reports", "q1-2021.pdf"]` our query will return the following response:

```json
{
  "document": {
    "allowAdminRoles": [
      "admin",
      "finance-admin",
      "reports-admin"
    ],
    "allowReadRoles": [
      "admin",
      "finance-admin",
      "reports-admin",
      "reports-reader"
    ],
    "allowWriteRoles": [
      "admin",
      "finance-admin",
      "reports-admin",
      "reports-writer"
    ],
    "endpoint": "/finance/reports/q2-2021.pdf"
  }
}
```

We now have all the data needed in order to have OPA determine if access should be granted or not.

```rego
package mongo

import rego.v1

default allow := false

resource_query_response := mongodb.find_one({
	"uri": "mongodb://localhost:27017",
	"database": "permissions",
	"collection": "resources",
	"filter": {"endpoint": sprintf("/%s", [concat("/", input.request.path)])},
	"options": {"projection": {"_id": false}},
})

# User is super admin — no need to query the database
admin if "admin" in input.user.roles

allow if admin

# User has role that grants admin privileges for endpoint
allow if {
	not admin
	some role in input.user.roles
	role in resource_query_response.document.allowAdminRoles
}

# Read request, and user has role that grants read privileges for endpoint
allow if {
	not admin
	input.request.method in {"GET", "HEAD"}
	some role in input.user.roles
	role in resource_query_response.document.allowReadRoles
}

# Write request, and user has role that grants write privileges for endpoint
allow if {
	not admin
	input.request.method in {"POST", "PUT"}
	some role in input.user.roles
	role in resource_query_response.document.allowWriteRoles
}
```

The above policy provides four conditions that will allow access to the resource:

- The user has the role `admin` — we'll consider this a "super admin" role which won't require querying the database
- The user has an admin role applicable to the resource requested, such as `reports-admin`
- The user is asking to read the resource, and has a role that grants read access to the resource
- The user is asking to write to the resource, and has a role that grants write access to the resource

If none of the above conditions are met, the request is denied.

Using the example `input.json` from above, we can try it out using `eopa eval`:

```shell
# terminal-command
eopa eval -f pretty -d policy.rego -i input.json data.mongo.allow
false
```

This makes sense, given that the request method was `PUT` and our roles included only `reports-reader`. If we change
the request method of the input to `GET`, the result should be access allowed:

```shell
# terminal-command
eopa eval -f pretty -d policy.rego -i input.json data.mongo.allow
true
```


## Finding all allowed endpoints

What if we wanted to know all the resources (or endpoints) a given user could access? We'd need a new rule for sure,
and the `input` to be slightly modified as well. Rather than requesting a specific resource, a request might instead
look something like this:

```json
{
  "operation": "read",
  "user": {
    "id": "alice",
    "roles": [
      "reports-reader"
    ]
  }
}
```

In other words — "given that I have the reports-reader role, what endpoints may I read?". Let's find out!
Since we're no longer requesting a single resource, we'll use the `mongodb.find` function for our query,
which may return any number of documents. Amending our policy with a few more rules, we'll end up with the
following:

```rego
operation_to_field := {
    "read": "allowReadRoles",
    "write": "allowReadRoles",
    "admin": "allowWriteRoles",
}

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
})

allowed_endpoints contains endpoint if {
    some resource in allowed_resources_query_response.documents
    endpoint := resource.endpoint
}
```

The first rule simply maps an `operation` from the `input` to the corresponding field used to match roles. The next rule
is the actual query. The `filter` attribute now uses a dynamic key, which will be one of `allowReadRoles`,
`allowWriteRoles` or `allowAdminRoles` depending on the `operation` provided in the `input`. We'll use the special
`$in` [operator](https://www.mongodb.com/docs/manual/reference/operator/query/in/) to match any of the roles provided
for the operation. This way, we'll get back all documents for which the user is allowed access given the provided
operation. The last rule, `allowed_endpoints`, simply filters out only the `endpoint` values and provides them in a set.
Given the previously provided input, we should get back a list of all endpoints our user is allowed to read:

```json
# terminal-command
eopa eval -f pretty -d mongo.rego -i input.json data.mongo.allowed_endpoints
[
  "/finance/reports",
  "/finance/reports/q1-2021.pdf",
  "/finance/reports/q2-2021.pdf"
]
```


## Authentication

In order to keep the examples above as simple as possible, we've omitted configuration for authentication.
For detailed configuration options, see the reference documentation for [mongodb functions](/enterprise-opa/reference/built-in-functions/mongodb)
