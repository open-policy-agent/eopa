---
sidebar_position: 10
sidebar_label: Local File
title: Local File Datasource Configuration | Enterprise OPA
---

# Local File Datasource Configuration

Enterprise OPA supports periodically loading data from a local file on disk. This makes prototyping more convenient, and allows non-networked, and host-specific data use cases. (Example: SSH's `host_identity.json` file)


## Example Configuration

The local file integration is provided via the `data` plugin, and needs to be enabled in Enterprise OPA's configuration.


### Minimal

```yaml
# enterprise-opa-conf.yaml
plugins:
  data:
    localfile.users:
      type: localfile
      file_path: users.json
      polling_interval: 5s
```

With this minimal configuration, Enterprise OPA will pull the `localfile.users` information
- from the relative path: `./users.json`,
- every 5 seconds,
- parsing the file contents as JSON.

Every 5 seconds, the entire file will be read into memory, and will have its hash computed.
If the hash is the same as the previous time the file was read, no parsing or data updates will happen, as the file contents did not change.

All of this can be configured using an advanced configuration:


### Advanced

```yaml
# enterprise-opa-conf-advanced.yaml
plugins:
  data:
    localfile.users:
      type: localfile
      file_path: example/users.txt
      file_type: json
      polling_interval: 5s
      rego_transform: "data.localfile.transform"
```

With a config like this, Enterprise OPA will read the text file, and attempt to parse it as JSON.

The result will then be available to all policy evaluations under `data.localfile.users`.

Supported file types include `xml`, `yaml`, and `json`. Unless the `file_type` key is provided, the plugin will assume the file type matches the file's extension (e.g. a `.json` file is of file type `json`).


## Example Setup

If the referenced file contains the following JSON document,
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
then Enterprise OPA's `data.localfile.users` will look like this:

```json
# terminal-command
curl "http://127.0.0.1:8181/v1/data/localfile/users?pretty"
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
The **key** below `data` in the configuration (`localfile.users` in the example) can be anything you want,
and determines where the retrieved document will be found in Enterprise OPA's `data` hierarchy.
:::


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from the local file into a different format for storage in Enterprise OPA.

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

And the content of our file on disk is:

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
curl "${ENTERPRISE_OPA_URL}/v1/data/localfile/users?pretty"
{
  "result": {
    "users": {
      "id01": "admin"
    }
  }
}
```
