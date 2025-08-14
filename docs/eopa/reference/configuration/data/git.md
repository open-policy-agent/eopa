---
sidebar_position: 7
sidebar_label: Git
title: Git Configuration | EOPA
---

# Git

EOPA's support for pulling in data from any Git repository makes it possible to
use GitOps practices for managing data and have that data available for policy
evaluations in EOPA.


## Example Configuration

The Git integration is provided via the `data` plugin, and needs to be enabled in EOPA's configuration.


### Minimal

```yaml
# eopa-conf.yaml
plugins:
  data:
    git.users:
      type: git
      url: https://git.internal.corp/data-repository
      file_path: users.json
```

With this minimal configuration, EOPA will pull in the `users.json` file from the
repository's `main` branch every 30 seconds.

All of this, and various authentication methods, can be configured using an advanced configuration:


### Advanced

```yaml
# eopa-conf-advanced.yaml
plugins:
  data:
    git.users:
      type: git
      url: https://git.internal.corp/data-repository
      file_path: users.json
      commit: 73b9d1aefab          # if empty, use `branch` (default: none)
      branch: prod-branch          # if empty, use `reference` (default: none)
      reference: ref/heads/main    # full git reference
      polling_interval: 10m        # default: 30s, minimum 10s

      username: alice              # basic auth
      password: wordpass           # basic auth

      token: personal-access-token # token auth

      private_key: path/to/key     # SSH key, file path or PEM contents
      passphrase: secret           # passphrase for protected keys

      rego_transform: data.e2e.transform
```

With a config like this, EOPA will retrieve the file from the specified
repository location, and attempt to parse as any of:
- XML
- YAML
- JSON

The result will then be available to all policy evaluations under `data.git.users`.


## Example Call

If the referenced Git repository contains a `users.json` file with this content,
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
then EOPA's `data.git.users` will look like this:

```json
# terminal-command
curl 'http://127.0.0.1:8181/v1/data/git/users?pretty'
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
The **key** below `data` in the configuration (`git.users` in the example) can be anything you want,
and determines where the retrieved document will be found in EOPA's `data` hierarchy.
:::


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from Git into a different format for storage in EOPA.

`rego_transform` policies take incoming messages as JSON via `input.incoming` and returns the transformed JSON.


### Example

Starting with the EOPA configuration above and the example data above

Our `data.e2e.transform` policy is:

```rego
package e2e

import rego.v1

transform.users := {name |
	some entry in input.incoming
	name := entry.username
}

transform.roles[id] := members if {
	some entry in input.incoming
	some role in entry.roles

	id := role

	members := role_members(id)
}

role_members(name) := {id |
	some entry in input.incoming
	name in entry.roles
	id := entry.username
}
```

Then the data retrieved by the Git plugin would be transformed by the above into:

```json
# terminal-command
curl "${EOPA_URL}/v1/data/git/users?pretty"
{
  "result": {
    "roles": {
      "admin": [
        "alice"
      ],
      "viewer": [
        "catherine"
      ]
    },
    "users": [
      "alice",
      "bob",
      "catherine"
    ]
  }
}
```
