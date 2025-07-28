---
sidebar_position: 5
sidebar_label: HTTP
title: HTTP Datasource Configuration | Enterprise OPA
---

# HTTP Datasource Configuration

Enterprise OPA's support for periodically pulling in data from any HTTP services makes it possible to
always have a snapshot of remote data available for policy evaluations in Enterprise OPA.


## Example Configuration

The HTTP integration is provided via the `data` plugin, and needs to be enabled in Enterprise OPA's configuration.


### Minimal

```yaml
# enterprise-opa-conf.yaml
plugins:
  data:
    http.users:
      type: http
      url: https://internal.example.com/api/users
```

With this minimal configuration, Enterprise OPA will pull the `http.users` information
- retrieved via `GET`,
- every 30 seconds,
- sending no request body,
- following redirects.

If the HTTP response contains an `ETag` header, it is sent on subsequent requests via `If-None-Match`.
Any responses with status 304 (Not Modified) will not cause data updates.

All of this can be configured using an advanced configuration:


### Advanced

```yaml
# enterprise-opa-conf-advanced.yaml
plugins:
  data:
    http.users:
      type: http
      url: https://internal.example.com/api/users
      method: POST            # default: GET
      body: '{"count": 1000}' # default: no body
      file: /some/file        # alternatively, read request body from a file on disk (default: none)
      timeout: "10s"          # default: no timeout
      polling_interval: "20s" # default: 30s, minimum: 10s
      follow_redirects: false # default: true
      headers:
        Authorization: Bearer XYZ
        other-header:         # multiple values are supported
        - value 1
        - value 2
      rego_transform: data.e2e.transform
```

With a config like this, Enterprise OPA will retrieve the document with the specified HTTP request,
and attempt to parse as any of:
- XML
- YAML
- JSON

The result will then be available to all policy evaluations under `data.http.users`.


## Example Call

If the referenced HTTP endpoint responds with the following JSON document,
```json
[
  {
    "username": "alice",
    "roles": [
      "admin"
    ]
  },
  {
    "username": "bob",
    "roles": []
  },
  {
    "username": "catherine",
    "roles": [
      "viewer"
    ]
  }
]
```
then Enterprise OPA's `data.http.users` will look like this:

```json
# terminal-command
curl "http://127.0.0.1:8181/v1/data/http/users?pretty"
{
  "result": [
    {
      "roles": [
        "admin"
      ],
      "username": "alice"
    },
    {
      "roles": [],
      "username": "bob"
    },
    {
      "roles": [
        "viewer"
      ],
      "username": "catherine"
    }
  ]
}
```

:::note
The **key** below `data` in the configuration (`http.users` in the example) can be anything you want,
and determines where the retrieved document will be found in Enterprise OPA's `data` hierarchy.
:::


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from the HTTP endpoint into a different format for storage in Enterprise OPA.

`rego_transform` policies take incoming messages as JSON via `input.incoming` and returns the transformed JSON.


### Example

If our `data.e2e.transform` policy is:

```rego
package e2e

import rego.v1

transform.users[id] := d if {
	entry := input.incoming
	id := entry.id
	d := entry.userId
}
```

And the normal response of the HTTP API endpoint is:

```json
{
  "userId": "admin",
  "id": "id01",
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}
```

Then the data retrieved by the HTTP API after transformation would be:

```json
# terminal-command
curl "${ENTERPRISE_OPA_URL}/v1/data/http/users?pretty"
{
  "result": {
    "users": {
      "id01": "admin"
    }
  }
}
```
