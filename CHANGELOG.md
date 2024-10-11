# Changelog

## Unreleased

This release includes a powerful new compiler optimization for Enterprise OPA: Loop-Invariant Code Motion!

### Loop-Invariant Code Motion

This technique allows Enterprise OPA to hoist redundant code out of loops in query plans, and allows it to intelligently restructure nested loops as well.
In iteration-heavy policies, the speedups can be dramatic.

This optimization is now enabled by default, so your policies will immediately benefit upon upgrading to the latest Enterprise OPA version.

## v1.27.1

[![OPA v0.69.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.69.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.69.0)
[![Regal v0.27.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.27.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.27.0)

This release includes various dependency bumps, as well as fixes for a performance regression affecting licensed Enterprise OPA users.


## v1.27.0

[![OPA v0.69.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.69.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.69.0)
[![Regal v0.27.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.27.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.27.0)

This release updates the OPA version used in Enterprise OPA to [v0.69.0](https://github.com/open-policy-agent/opa/releases/tag/v0.69.0).

It also includes various dependency bumps.


## v1.26.0

[![OPA v0.68.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.68.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.68.0)
[![Regal v0.27.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.27.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.27.0)

This release contains various version bumps and an improvement to EKM ergonomics!


### External Key Manager (EKM): Simplified configuration, support for plugin configs

Starting with this release, you no longer need to reference _service_ and _keys_ replacements via JSON pointers, but you can use direct lookups, like

```yaml
services:
  acmecorp:
    credentials:
      bearer:
        scheme: "bearer"
        token: "${vault(kv/data/acmecorp/bearer:data/token)}"
```

Furthermore, these are **also supported in plugins** allowing you to retrieve secrets for their configurations as well.

These replacement can also be done in substrings, like this:

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: http
      url: https://myservice.corp.com/v1/logs
      headers:
        Authorization: "bearer ${vault(kv/data/logs:data/token)}"
```

Replacements also happen on discovery bundles, if their config includes lookup calls of this sort.

[See here for the docs on _Using Secrets from HashiCorp Vault_](https://docs.styra.com/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault).


## v1.25.1

[![OPA v0.68.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.68.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.68.0)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This release contains optimizations for the [Batch Query API](https://docs.styra.com/enterprise-opa/reference/api-reference/batch-api).


## v1.25.0

[![OPA v0.68.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.68.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.68.0)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This release updates the OPA version used in Enterprise OPA to [v0.68.0](https://github.com/open-policy-agent/opa/releases/tag/v0.68.0).

It also includes various dependency bumps.


## v1.24.8

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This release upgrades the `common_input` field for the [Batch Query API](https://docs.styra.com/enterprise-opa/reference/api-reference/batch-api) to support recursive merges with each per-query input. This is expected to allow further reduction of request sizes when the majority of each query's input would be shared data.

Here is an example of the recursive merging in action:
```json
{
  "inputs": {
    "A": {
      "user": {
        "name": "alice",
        "type": "admin"
      },
      "action": "write",
    },
    "B": {
      "user": {
        "name": "bob",
        "type": "employee"
      }
    },
    "C": {
      "user": {"name": "eve"}
    }
  },
  "common_input": {
    "user": {
      "company": "Acme Corp",
      "type": "user",
    },
    "action": "read",
    "object": "id1234"
  }
}
```

The above request using `common_input` is equivalent to sending this request:
```json
{
  "inputs": {
    "A": {
      "user": {
        "name": "alice",
        "company": "Acme Corp",
        "type": "admin"
      },
      "action": "write",
      "object": "id1234"
    },
    "B": {
      "user": {
        "name": "bob",
        "company": "Acme Corp",
        "type": "employee"
      },
      "action": "read",
      "object": "id1234"
    },
    "C": {
      "user": {
        "name": "eve",
        "company": "Acme Corp",
        "type": "user",
      },
      "action": "read",
      "object": "id1234"
    }
  }
}
```

In the event of matching keys between the `common_input` and the per-query `input` object, the per-query input's value is used.
This behavior is intentionally like the [behavior of `object.union` in Rego](https://www.openpolicyagent.org/docs/latest/policy-reference/#builtin-object-objectunion).


## v1.24.7

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This release contains a new optional field for [Batch Query API](https://docs.styra.com/enterprise-opa/reference/api-reference/batch-api) requests: `common_input`.
This field allows factoring out common top-level keys in an input object, which can greatly reduce request sizes in some cases.

Here is an example:
```json
{
  "inputs": {
    "A": {
      "user": "alice",
      "action": "write",
    },
    "B": {
      "user": "bob"
    },
    "C": {
      "user": "eve"
    }
  },
  "common_input": {
    "action": "read",
    "object": "id1234"
  }
}
```

The above request using `common_input` is equivalent to sending this request:
```json
{
  "inputs": {
    "A": {
      "user": "alice",
      "action": "write",
      "object": "id1234"
    },
    "B": {
      "user": "bob",
      "action": "read",
      "object": "id1234"
    },
    "C": {
      "user": "eve",
      "action": "read",
      "object": "id1234"
    }
  }
}
```

### Conflict resolution

In cases where the types are both JSON Objects, the objects' top-level keys will be merged non-recursively.
In the event of a conflict where both `common_input` and the per-query input have the same key, the per-query input's key/value pair is used, as shown in the earlier example where `common_input` provides the `"action": "read"` key/value pair, and query `"A"` provides `"action": "write"` for the same top-level key/value pair.

In cases where the `common_input`'s type conflicts with that of the per-query input, the per-query input value is used.

Example:
```json
{
  "inputs": {
    "A": [1, 2, 3]
  },
  "common_input": {
    "foo": "bar"
  }
}
```

The above example is equivalent to the following request, because the input type overrides:
```json
{
  "inputs": {
    "A": [1, 2, 3]
  }
}
```


## v1.24.6

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This patch contains optimizations and bugfixes for the [Batch Query API](https://docs.styra.com/enterprise-opa/reference/api-reference/batch-api) when used with [OPA authorization policies](https://www.openpolicyagent.org/docs/latest/security/#authentication-and-authorization).


## v1.24.5

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.25.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.25.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.25.0)

This patch contains optimizations for the [Batch Query API](https://docs.styra.com/enterprise-opa/reference/api-reference/batch-api), and also updates the Regal version used in Enterprise OPA to version [v0.25.0](https://github.com/StyraInc/regal/releases/tag/v0.25.0).


## v1.24.4

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.24.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.24.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

This patch contains experimental features for logging intermediate evaluation results. This feature is not generally available at this time, and is disabled during normal use.


## v1.24.3

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.24.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.24.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

This release updates Enterprise OPA to allow for environment variable substitution for the config produced by the discovery bundle. This release also updates some dependencies.

For example, with the environment variable `ENV1=test1`, and this config is used via discovery:

```yaml
bundle:
    name: ${ENV1}
decision_logs: {}
status: {}
```

Enterprise OPA would interpret the configuration like so:

```yaml
bundle:
    name: test1
decision_logs: {}
status: {}
```

## v1.24.2

[![OPA v0.67.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1)
[![Regal v0.24.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.24.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

This release updates the OPA version used in Enterprise OPA to [v0.67.1](https://github.com/open-policy-agent/opa/releases/tag/v0.67.1), which includes a bugfix for chunked request handling.


## v1.24.1

[![OPA v0.67.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.0)
[![Regal v0.24.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.24.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

This release fixes a regression in the Enterprise OPA CLI help text on some commands, and includes several updates to our dependencies.


## v1.24.0

[![OPA v0.67.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.67.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.67.0)
[![Regal v0.24.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.24.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

This release updates the OPA version used in Enterprise OPA to [v0.67.0](https://github.com/open-policy-agent/opa/releases/tag/v0.67.0), and updates Regal to [v0.24.0](https://github.com/StyraInc/regal/releases/tag/v0.24.0)

The OPA version bump includes max request body size limits (a potentially breaking change for clients who use enormous request sizes), optimizations around request handling, and improved performance under load for gzipped requests.


## v1.23.0

[![OPA v0.66.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.66.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.66.0)
[![Regal v0.23.1](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.23.1&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.23.1)

It also updates the OPA version used in Enterprise OPA to [v0.66.0](https://github.com/open-policy-agent/opa/releases/tag/v0.66.0), and brings in various dependency bumps.

The OPA version bump includes memory usage improvements when loading gigantic bundles.


## v1.22.0

[![OPA v0.65.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.65.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.65.0)
[![Regal v0.22.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.22.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.22.0)

This release includes an new Apache Pulsar data source, a Bulk Eval HTTP API for evaluating a policy with multiple inputs in one request, and performance improvements when loading large bundles.

It also updates the OPA version used in Enterprise OPA to [v0.65.0](https://github.com/open-policy-agent/opa/releases/tag/v0.65.0), and brings in various dependency bumps.

### Pulsar Data Source

Enterprise OPA can now subscribe to Apache Pulsar topics:

```yaml
# enterprise-opa.yaml
plugins:
  data:
    users:
      type: pulsar
      url: pulsar://pulsar.corp.com:6650
      topics:
      - users
      rego_transform: "data.pulsar.transform"
```

[See the docs for more information.](https://docs.styra.com/enterprise-opa/reference/configuration/data/pulsar)

### Bulk HTTP API

You can now do multiple policy evaluations in one request:

```http
POST /v1/batch/data/policy/allow
{
  "inputs": {
    "id-1": {
      "user": "alice",
      "action": "read",
      "resource": "book"
    },
    "id-2": {
      "user": "alice",
      "action": "create",
      "resource": "book"
    },
    "id-3": {
      "user": "alice",
      "action": "delete",
      "resource": "book"
    }
  }
}
```
The response looks like this:

```json
{
  "responses": {
    "id-1": {
      "result": true
    },
    "id-1": {
      "result": true
    },
    "id-3": {
      "result": false
    },
  }
}
```

It supports the standard query parameters (like `pretty`, `metrics`, `strict-builtin-errors`).

### Very large bundles performance

Previously, activating a bundle did some unneeded work.
It became apparent, and problematic, when using very large bundles (1+ GB).
The issue has been fixed, leading to noticable performance improvements when using very large bundles.


## v1.21.0

[![OPA v0.64.1](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.64.1)](https://github.com/open-policy-agent/opa/releases/tag/v0.64.1)
[![Regal v0.21.3](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.21.3&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.21.3)

This release includes an enhancement to the Apache Kafka data source, and updates the OPA version used in Enterprise OPA to [v0.64.1](https://github.com/open-policy-agent/opa/releases/tag/v0.64.1), and brings in various dependency bumps.

### Kafka data source: prometheus metrics (per-instance)

Each instance of the Kafka data plugin now contributes a bunch of Prometheus metrics to the global metrics endpoint:

    kafka_MOUNTPOINT_METRIC

Where MOUNTPOINT is `foo:bar` for a Kafka data plugin configured to manage `data.foo.bar`. (The Prometheus metrics naming restrictions forbid
both "." and "/" in metric names.)

### Kafka data source: logging enhancements

When run with log level "debug", the low-level Kafka logs are overwhelming most of the times.
They are now suppressed by default, and can be inspected when running with the environment variable `EOPA_KAFKA_DEBUG`, like:

    EOPA_KAFKA_DEBUG=1 eopa run -s -ldebug -c eopa.yaml transform.rego

In addition to that, the _consumer group_ (if configured) is now logged when the data source plugin is initiated.
Also, new key/value fields were introduced to read the batch size and transformation time from the logs more easily.

### VM: builtin function `json.unmarshal` is now natively implemented

This improves the performance by lowered data conversion overheads.
This, too, benefits Kafka transforms because they always include a `json.unmarshal` call.

## v1.20.0

[![OPA v0.63.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.63.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.63.0)
[![Regal v0.19.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.19.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.19.0)

This release includes an enhancement to the Apache Kafka data source, updates the OPA version used in Enterprise OPA to [v0.63.0](https://github.com/open-policy-agent/opa/releases/tag/v0.63.0), and brings in various dependency bumps.

### Kafka data source: consumer group support

By providing `consumer_group: true` in the Kafka data source configuration, Enterprise OPA will register the data plugin instance as its own _consumer group_ with the Kafka Broker.
This improves observability of the Kafka data plugin, since you can now use standard Kafka tooling to determine the status of your consuming Enterprise OPA instances, including the number of messages they lag behind.

Due to the way consumer groups work, each data plugin instance will form its own _one-member_ consumer group.
The group name includes the Enterprise OPA instance ID, which is reset on restarts.
These two measures guarantee that the message consumption behaviour isn't changed: each (re)started instance of Enterprise OPA will read all the messages of the topic, unless configured otherwise.

For details, see [the Kafka data source documentation](https://docs.styra.com/enterprise-opa/reference/configuration/data/kafka#consumer-group).

### Kafka data source: show `print()` output on errors

When a data source Rego transform _fails_, it can be difficult to debug, even more so when it depends on hard-to-reproduce message batches coming in from Apache Kafka.
To help with this, any `print()` calls in Rego transforms are now emitted, even if the overall transformation failed, e.g. with an object insertion conflict.

## v1.19.0

[![OPA v0.62.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.62.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.62.0)
[![Regal v0.19.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.19.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.19.0)

This release includes a few new features around test generation, as well as a Regal version bump.

### Test Stub Generation

It is now possible to quickly spin up a test suite for a policy project with Enterprise OPA, using the new test generation commands: `test bootstrap` and `test new`.

These commands will generate test stubs that are pre-populated with `input` objects, based off of what keys each rule body references from the input.
While the stubs usually need some customization after generation in order to match the exact policy constraints, the generation commands remove much of the initial boilerplate work required for basic test coverage.

#### Bootstrapping a starting set of test stubs (one test group per rule body)

Given the file `example.rego`:
```rego
package example

import rego.v1

servers := ["dev", "canary", "prod"]

default allow := false

allow if {
  input.servers.names[_] == data.servers[_]
  input.action == "fetch"
}
```

We can generate a set of basic tests for the `allow` rules using the command: `eopa test bootstrap -d example.rego example/allow`

The generated tests will appear in a file called `example_test.rego`, and should look roughly like the following:
```rego
package example_test

import rego.v1

# Testcases generated from: example.rego:7
# Success case: All inputs defined.
test_success_example_allow_0 if {
	test_input = {"input": {}}
	data.example.allow with input as test_input
}
# Failure case: No inputs defined.
test_fail_example_allow_0_no_input if {
	test_input = {}
	not data.example.allow with input as test_input
}
# Failure case: Inputs defined, but wrong values.
test_fail_example_allow_0_bad_input if {
	test_input = {"input": {}}
	not data.example.allow with input as test_input
}


# Testcases generated from: example.rego:9
# Success case: All inputs defined.
test_success_example_allow_1 if {
	test_input = {"input": {"action": "EXAMPLE", "servers": {"names": "EXAMPLE"}}}
	data.example.allow with input as test_input
}
# Failure case: No inputs defined.
test_fail_example_allow_1_no_input if {
	test_input = {}
	not data.example.allow with input as test_input
}
# Failure case: Inputs defined, but wrong values.
test_fail_example_allow_1_bad_input if {
	test_input = {"input": {"action": "EXAMPLE", "servers": {"names": "EXAMPLE"}}}
	not data.example.allow with input as test_input
}
```

#### Adding new named test stubs

If we add a new rule to the policy with an [OPA metadata annotation](https://www.openpolicyagent.org/docs/latest/policy-language/#metadata) `test-bootstrap-name`:
```rego
# ...

# METADATA
# custom:
#   test-bootstrap-name: allow_admin
allow if {
	"admin" in input.user.roles
}
```

We can then add generated tests for this new rule to the test file with the command `eopa test new -d example.rego 'allow_admin'`
The new test will be appended at the end of test file, and will look like:

```rego
# ...

# Testcases generated from: example.rego:17
# Success case: All inputs defined.
test_success_allow_admin if {
	test_input = {"input": {"user": {"roles": "EXAMPLE"}}}
	data.example.allow with input as test_input
}
# Failure case: No inputs defined.
test_fail_allow_admin_no_input if {
	test_input = {}
	not data.example.allow with input as test_input
}
# Failure case: Inputs defined, but wrong values.
test_fail_allow_admin_bad_input if {
	test_input = {"input": {"user": {"roles": "EXAMPLE"}}}
	not data.example.allow with input as test_input
}
```

The metadata annotation allows control over test naming with both the `bootstrap` and `new` commands.
If two rules have the same metadata annotation, an error message will report the locations of the conflicts.


## v1.18.0

[![OPA v0.62.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.62.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.62.0)
[![Regal v0.17.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.17.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.17.0)

This release includes updates to embedded OPA and Regal versions, and various
bug fixes and dependency bumps. It also include some telemetry enhancements.

### OPA v0.62.0 and Regal v0.17.0

This release updates the OPA version used in Enterprise OPA to [v0.62.0](https://github.com/open-policy-agent/opa/releases/tag/v0.62.0),
and the embedded Regal version (used with `eopa lint`) to [v0.17.0](https://github.com/StyraInc/regal/releases/tag/v0.17.0).

### `eopa login` and `eopa pull` prepare .styra.yaml for `eopa run`

Previously, an extra step was necessary to have `eopa run` pick up DAS
libraries pulled in via `eopa pull`. Now, the generated configuration already
includes all the necessary settings for a seamless workflow of:

- `eopa login --url https://my-tenant.styra.com`
- `eopa pull`
- `eopa run --server`

See [_How to develop and test policies locally using Styra DAS libraries_](https://docs.styra.com/enterprise-opa/how-to/develop-and-test-applications-using-libraries)
for details.

### OPA compatibility when querying `data`

Previously, Enterprise OPA would include the `data.system` tree in queries for
`data` -- either via the CLI, `eopa eval data` or via the HTTP API,
`GET /v1/data`. That isn't harmful, but it differs from what OPA does.
Now, Enterprise OPA will give the same results -- omitting `data.system`.

### Telemetry

Enterprise OPA now reports on the type of bundles used: delta/snapshot and JSON
or BJSON, to help prioritizing future work.

## v1.17.2

This release fixes an issue with using OPA fallback mode (when missing a license) together with bundles.

### Regal v0.16.0

The embedded Regal version (used with `eopa lint`) was updated to [v0.16.0](https://github.com/StyraInc/regal/releases/tag/v0.16.0).

## v1.17.1

This release addresses an issue where YAML files in a bundle could cause Enterprise OPA to return an error, particularly during `eopa eval`.

## v1.17.0

### Regal Linting Support

Enterprise OPA now integrates the powerful [Regal](https://github.com/StyraInc/regal) linter for Rego policies!

For example, if you had the example policy from the Regal docs:

`policy/authz.rego`:
```rego
package authz

import future.keywords

default allow = false

deny if {
    "admin" != input.user.roles[_]
}

allow if not deny
```

You can lint the policy with `eopa lint` as follows:

```bash
$ eopa lint policy/
Rule:         	not-equals-in-loop
Description:  	Use of != in loop
Category:     	bugs
Location:     	policy/authz.rego:8:13
Text:         	"admin" != input.user.roles[_
Documentation:	https://docs.styra.com/regal/rules/bugs/not-equals-in-loop

Rule:         	use-assignment-operator
Description:  	Prefer := over = for assignment
Category:     	style
Location:     	policy/authz.rego:5:1
Text:         	default allow = false
Documentation:	https://docs.styra.com/regal/rules/style/use-assignment-operator

Rule:         	prefer-some-in-iteration
Description:  	Prefer `some .. in` for iteration
Category:     	style
Location:     	policy/authz.rego:8:16
Text:         	"admin" != input.user.roles[_
Documentation:	https://docs.styra.com/regal/rules/style/prefer-some-in-iteration

1 file linted. 3 violations found.
```

### DAS Workflow Support

You can now pull down policies and libraries from a [Styra DAS](https://docs.styra.com/das) instance, allowing easier local testing and development.

To start the process, run `eopa login`, like in the example below.

    eopa login --url https://example.styra.com

This will bring up an OAuth login screen, which will allow connecting your local Enterprise OPA instance to your company's DAS instance.
Once your Enterprise OPA instance is authenticated, you can then pull down the policies from your DAS Workspace using `eopa pull`.

    eopa pull

This will store the policies and library code from DAS under a folder named `.styra/include/libraries` by default.


## v1.16.0

[![OPA v0.61.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.61.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.61.0)

This release updates the OPA version used in Enterprise OPA to [v0.61.0](https://github.com/open-policy-agent/opa/releases/tag/v0.61.0),
and includes telemetry enhancements, bug fixes, and various dependency updates.

### Huge floats

Gigantic floating point numbers (like `23456789012E667`) no longer cause a panic in the VM.

### Telemetry

Enterprise OPA now includes the latest retrieved bundle sizes, and the number of datasource plguins
that are used, to help prioritizing future work.


## v1.15.5

This release contains a bugfix for an issue where some Rego policies querying the entirety of the `data` namespace could see incorrect results.

## v1.15.2, v1.15.3, v1.15.4

These releases are release engineering improvements and fixes for our automated publishing of artifacts, such as capabilities JSON files, gRPC protobuf definitions, and more.

## v1.15.1

[![OPA v0.60.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.60.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.60.0)

This is a bug fix release for an exception that occurred when using a
per-output mask or drop decision that included a print() statement.

It's only relevant to you if
- you are using the `eopa_dl` decision logs plugin,
- with a per-output mask_decision or drop_decision,
- and that decision includes a `print()` call.


## v1.15.0

[![OPA v0.60.0](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/v0.60.0)](https://github.com/open-policy-agent/opa/releases/tag/v0.60.0)

This release updates the OPA version used in Enterprise OPA to [v0.60.0](https://github.com/open-policy-agent/opa/releases/tag/v0.60.0),
and includes improvements for Decision Logging, `sql.send`, and the `eopa eval`
experience.

### Contextual information on errors in `eopa eval`

When you evaluate a policy, `eopa eval --format=pretty` will include extra links to
docs pages explaining the errors, and how to overcome them.

For example, with a policy like
```rego
# policy.rego
package policy

allow := data[input.org].allow
```

```interactive
$ eopa eval -fpretty -d policy.rego data.policy.allow
1 error occurred: policy.rego:3: rego_recursion_error: rule data.policy.allow is recursive: data.policy.allow -> data.policy.allow
For more information, see: https://docs.styra.com/opa/errors/rego-recursion-error/rule-name-is-recursive
```

Note that the output only appears on standard error, and only for output format
"pretty", so it should not interfere with any scripted usage of `eopa eval` you
may have.

### Decision Logs: per-output mask and drop decisions

Enterprise OPA lets you configure multiple sinks for your decision logs.
With this release, you can also specific per-output `mask_decision` and `drop_decision`
settings, to accomodate different privacy and data restrictions.

For example, this configuration would apply a mask decision (`data.system.s3_mask`)
only for the S3 sink, and a drop decision (`data.system.console_drop`) for the console
output.

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: memory
    output:
    - type: console
      drop_decision: system/console_drop
    - type: s3
      mask_decision: system/s3_mask
      # more config
```

Also see
- [Decision Logs Configuration](https://docs.styra.com/enterprise-opa/reference/configuration/decision-logs)
- [Tutorial: Logging decisions to AWS S3](https://docs.styra.com/enterprise-opa/tutorials/decision-log-s3)
- [Masking and dropping decision logs](https://www.openpolicyagent.org/docs/latest/management-decision-logs/) from the OPA docs.

### `sql.send` supports MS SQL Server

`sql.send` now supports Microsoft SQL Server! To connect to it, use a `data_source_name` of

    sqlserver://USER:PASSWORD@HOST:PORT?database=DATABASE_NAME

For complete description of `data_source_name` options available, see: https://github.com/microsoft/go-mssqldb#connection-parameters-and-dsn

It also comes with the usual Vault helpers, under `system.eopa.utils.sqlserver.v1.vault`.

See [the `sql.send` documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/sql)
for all details.

### Telemetry

Telemetry data sent to Styra's telemetry system now includes the License ID.
You can use `eopa run --server --disable-telemetry` to **opt-out**.

## v1.14.0

This release updates the OPA version used in Enterprise OPA to [v0.59.0](https://github.com/open-policy-agent/opa/releases/tag/v0.59.0),
and integrates some performance improvements and a few bug fixes.

### CLI

 - Fixed a panic when running `eopa bundle convert` on [Delta Bundles](https://www.openpolicyagent.org/docs/latest/management-bundles/#delta-bundles).

### Runtime

 - The Set and Object types received a few small performance optimizations in this release, which net out speedups of around 1-7% on some benchmarks.
 - Set union operations are slightly faster now.


## v1.13.0

This release contains a security fix for gRPC handlers used with OpenTelemetry, various performance
enhancements, bug fixes, third-party dependency updates, and a way to have Enterprise OPA fall back
to "OPA-mode" when there is no valid license.

### OpenTelemetry CVE-2023-47108

This release updates the gRPC handlers used with OpenTelemetry to address a security vulnerability (CVE-2023-47108, https://github.com/advisories/GHSA-8pgv-569h-w5rw).

### Fallback to OPA

When using `eopa run` and `eopa exec` without a valid license, Enterprise OPA will now log a message,
and continue executing as if it was an ordinary instance of OPA.

This is enabled by running the license check synchronously. It'll be quick for missing files and environment
variables.

**If you don't want to fallback to OPA**, because you expect your license to be present and valid, you can
pass `--no-license-fallback` to both `eopa run` and `eopa exec`: the license validation will run asynchronously,
and stop the process on failures.

### Bug Fixes

1. The gRPC API's decision logs now include the `input` sent with the request.
2. An issue with the `mongodb.find` and `mongodb.find_one` caching has been resolved.


## v1.12.0

This release updates the OPA version used in Enterprise OPA to [v0.58.0](https://github.com/open-policy-agent/opa/releases/tag/v0.58.0),
and integrates several performance improvements and a bug fix:

### Function return value caching

Function calls in Rego now have their return value cached: when called with the same arguments,
subsequent evaluations will use the cached value.
Previously, the function body was evaluated twice.

Currently, only simple argument types are subject to caching: numbers, bools, strings -- collection
arguments are exempt.

### Library utils lazy loading

If your policy does not make use of any of the `data.system.eopa.utils` helpers of Enterprise OPA's
[builtin functions](https://docs.styra.com/enterprise-opa/reference/built-in-functions), they are not loaded,
and thus avoid superfluous work in the compiler.

### Topdown-specific compiler stages

When evaluating a policy, certain compiler stages in OPA are now skipped: namely, the Rego VM in
Enterprise OPA does not make use of OPA's rule and comprehension indices, so we no longer build them
in the compiler stages.

### Numerous Rego VM improvements

The Rego VM now uses less allocations, improving overall performance.

### [Preview API](https://docs.styra.com/enterprise-opa/reference/api-reference/preview-api)

Fixes a bug with "Preview Selection".


## v1.11.1

This is a bug fix release addressing the following security issue:

OpenTelemetry-Go Contrib security fix [CVE-2023-45142](https://github.com/advisories/GHSA-rcjv-mgp8-qvmr):

 > Denial of service in otelhttp due to unbound cardinality metrics.

### Note: [GO-2023-2102](https://pkg.go.dev/vuln/GO-2023-2102) was fixed in v1.11.0

> A malicious HTTP/2 client which rapidly creates requests and immediately resets them can cause excessive server resource consumption.

## v1.11.0

This release includes several bugfixes and a powerful new feature for data source integrations: Rego transform rules!

### Transform rules for all data source integrations

Enterprise OPA now supports Rego transform rules for all data source plugins!
These transform rules allow you to reshape and modify data fetched by the data sources, *before* that data is stored in EOPA for use by policies.

This feature can be opted into for a data source by adding a `rego_transform` key to its YAML configuration block.

#### Example transform rule with the HTTP data source

For this example, we will assume we have an HTTP endpoint that responds with the following JSON document:
```json
[
    {"username": "alice", "roles": ["admin"]},
    {"username": "bob", "roles": []},
    {"username": "catherine", "roles": ["viewer"]}
]
```

Here's what the OPA configuration might look like for a fictitious HTTP data source:

```yaml
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

The `rego_transform` key at the end means that we will run the `data.e2e.transform` Rego rule on the incoming data *before* that data is made available to policies on this EOPA instance.

We then need to define our `data.e2e.transform` rule.
`rego_transform` rules generally take incoming messages as JSON via `input.incoming` and return the transformed JSON for later use by other policies.
Below is an example of what a transform rule might look like for our HTTP data source:

```rego
package e2e
import future.keywords
transform.users[id] := d {
  some entry in input.incoming
  id := entry.username
  d := entry.roles
}
```

In the above example, the transform policy will to populate the `data.http.users` object with key-value pairs.
Each key-value pair will be generated by iterating across the JSON list in `input.incoming`, and for each JSON object, the key will be taken from the `username` field, and the value from the `roles` field.

Given our earlier data source, the result stored in EOPA for `data.http.users` will look like:

```json
{
    "alice": ["admin"],
    "bob": [],
    "catherine": ["viewer"]
}
```

This general pattern applies to all the data source integrations in Enterprise OPA, including the Kafka data source (covered below).

### Updates to the Kafka data source's Rego transform rules

The Kafka data source now supports the new `rego_transform` rule system, similar to the other major data source integrations.
The main difference for new message transformation rules is the use of specialized variables to refer to new and existing Kafka data: `input.incoming` and `input.previous`.

`input.incoming` represents the batch of incoming new Kafka messages, and `input.previous` refers to everything stored previously by the Kafka data source.
This allows constructing data source transform rules in a straightforward way.

See the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/configuration/data/kafka) for more details and examples of the new transform rules.

### Updates to the `dynamodb` series of builtins

In this release `dynamodb.send` has been split apart into more specialized variants embodying the same functionality: `dynamodb.get` and `dynamodb.query`.

#### `dynamodb.get`

For normal key-value lookups in DynamoDB, `dynamodb.get` provides a straightforward solution.
Here is a brief usage example:

```rego
thread := dynamodb.get({
  "endpoint": "dynamodb.us-west-2.amazonaws.com",
  "region": "us-west-2",
  "table": "thread",
  "key": {
      "ForumName": {"S": "help"},
      "Subject": {"S": "How to write rego"}
  }
}) # => { "row": ...}
```

See the [Reference documentation][eopa-docs-dynamodb-get] for more details.

#### `dynamodb.query`

For queries on DynamoDB, `dynamodb.query` allows control over the query expression and other parameters:
Here is a brief usage example:

```rego
music := dynamodb.query({
  "region": "us-west-1",
  "table": "foo",
  "key_condition_expression": "#music = :name",
  "expression_attribute_values": {":name": {"S": "Acme Band"}},
  "expression_attribute_names": {"#music": "Artist"}
}) # => { "rows": ... }
```

See the [Reference documentation][eopa-docs-dynamodb-query] for more details.

   [eopa-docs-dynamodb-get]: https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb#dynamodbget
   [eopa-docs-dynamodb-query]: https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb#dynamodbquery

## v1.10.1

### New data source integration: MongoDB

It is now possible to use a single MongoDB collection as a data source, with optional filtering/projection at retrieval time.

For example if you had `collection1` in a MongoDB instance set to the following JSON document:

```json
[
  {"foo": "a", "bar": 0},
  {"foo": "b", "bar": 1},
  {"foo": "c", "bar": 0},
  {"foo": "d", "bar": 3}
]
```

If you configured a MongoDB data source to use `collection1`:

```yaml
plugins:
  data:
    mongodb.example:
      type: mongodb
      uri: <your_db_uri_here>
      auth: <your_login_info_here>
      database: database
      collection: collection1
      keys: ["foo"]
      filter: {"bar": 0}
```

The configuration shown above would filter this collection down to just:
```json
[
  {"foo": "a", "bar": 0},
  {"foo": "c", "bar": 0}
]
```

The `keys` parameter in the configuration shown earlier guides how the collection is transformed into a Rego Object, mapping the unique key field(s) to the corresponding documents from the filtered collection:

```json
{
  "a": {"foo": "a", "bar": 0},
  "c": {"foo": "c", "bar": 0}
}
```

You could then use this data source in a Rego policy just like any other aggregate data type. As a simple example:

```rego
package hello_mongodb

filtered_documents := data.mongodb.example

allow if {
  count(filtered_documents) == 2 # Want just 2 items in the collection.
}
```

## v1.10.0

This release updates the OPA version used in Enterprise OPA to [v0.57.0](https://github.com/open-policy-agent/opa/releases/tag/v0.57.0), and integrates several bugfixes and new features.

## v1.9.1, v1.9.2, v1.9.3, v1.9.4, v1.9.5

These releases have been release engineering fixes to sort out automated publishing of this changelog, capabilities JSON files, and gRPC protobuf definitions.

## v1.9.0

This release updates the OPA version used in Enterprise OPA to [v0.56.0](https://github.com/open-policy-agent/opa/releases/tag/v0.56.0), and integrates several bugfixes and new features.

### `mongodb.find`, `mongodb.find_one`: query MongoDB databases during policy evaluation

Enterprise OPA now supports querying MongoDB databases!

Two new builtins are dedicated for this purpose: `mongodb.find`, and `mongodb.find_one`. These correspond approximately to MongoDB's [`db.collection.find()`](https://www.mongodb.com/docs/manual/reference/method/db.collection.find/) and [`db.collection.findOne()`](https://www.mongodb.com/docs/manual/reference/method/db.collection.findOne/) operations, respectively. These operations make it possible to integrate MongoDB databases efficiently into policies, depending on whether a single or multiple document lookup is needed.

Find out more in the new [Tutorial](https://docs.styra.com/enterprise-opa/tutorials/querying-mongodb), or see the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/mongodb) for more details.

### `dyanmodb.send`: query DynamoDB during policy evaluation

This builtin currently supports sending [GetItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_GetItem.html) and [Query](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Query.html) requests to a DynamoDB endpoint, allowing direct integration of DynamoDB into policies.

Find out more in the new [Tutorial](https://docs.styra.com/enterprise-opa/tutorials/dynamodb-send), or see the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb) for more details.

### `vault.send` for interacting directly with Hashicorp Vault in policies.

This new builtin provides support for more direct, request-oriented Hashicorp Vault integrations in policies than was previously possible through the [EKM Plugin](https://docs.styra.com/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault).

See the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/vault) for more details.

### gRPC plugin Decision Logs Support

The gRPC server plugin now integrates into Enterprise OPA's decision logging!
This means that gRPC requests are logged in a near-identical format to HTTP requests, allowing deeper insight into the usage and performance of Enterprise OPA deployments in production.

## v1.8.0

This release updates the OPA version used in Enterprise OPA to
[v0.55.0](https://github.com/open-policy-agent/opa/releases/tag/v0.55.0).

## v1.7.0

### Envoy External Authorization Support

This release makes Styra Enterprise OPA a drop-in replacement for
[opa-envoy-plugin](https://github.com/open-policy-agent/opa-envoy-plugin/), to be
used with the [External Authorization feature](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/security/ext_authz_filter.html)
of the popular Envoy API gateway, and Envoy-based service meshes such as Istio
and Gloo Edge.

It works exactly like `opa-envoy-plugin``, i.e. the images known as `openpolicyagent/opa:latest-envoy`,
but featuring all the Enterprise OPA enhancements.

[See here](https://www.openpolicyagent.org/docs/latest/envoy-introduction/) for a general
introduction to OPA and Envoy.

### Enhanced OpenTelemetry Support

Styra Enterprise OPA now supports OpenTelemetry Traces for the following operations:

- Rego VM evaluations, with extra spans for `http.send` and `sql.send`
- All decision log operations.
- All of its gRPC handlers.

This allows for improved observability, allowing you to quicker pin-point any issues
in your distributed authorization system.

## v1.6.0

This release updates the OPA version used in Styra Enterprise OPA to
[v0.54.0](https://github.com/open-policy-agent/opa/releases/tag/v0.54.0),
along with gRPC plugin improvements and new gRPC streaming endpoints.

### Support for large gRPC message sizes

Most gRPC implementations default to having a max receivable message size of 4 MB for both servers and clients.
This helps avoid memory exhaustion from large messages sent by misconfigured or malicious actors on the other side of the connection.

This size limit presents a problem for Enterprise OPA though: a relatively simple rule query that returns a large amount of data can easily break past that 4 MB message size limit.
Additionally, clients who need to provide more than 4 MB of data for a data update or rule query input can also run into the receivable message size limit.
To work around this problem, we have to attack it from both the client and server sides.

On the client side, most gRPC implementations allow providing the "Max Receive Message Size" as a parameter for the gRPC call. (See the [`CallOption.MaxRecvMsgSize`](https://pkg.go.dev/google.golang.org/grpc#MaxCallRecvMsgSize) option in Go, for example.)
This means that clients who want to receive potentially massive responses from the Enterprise OPA server will need to do a little more setup at call time, but don't necessarily need to change their Enterprise OPA configs.

For the server side of the problem, we changed Enterprise OPA to support a new configuration option for the gRPC plugin: `grpc.max_recv_message_size`

In the example configuration below, we start up the Enterprise OPA gRPC server on `localhost:9090`, and set it to receive messages up to 8 MB in size:
```yaml
plugins:
  grpc:
    # 8 MB, in bytes:
    max_recv_message_size: 8589934592
    addr: "localhost:9090"
```

This allows the server to receive larger gRPC messages from clients than before.

Fixing both sides of the large gRPC message size problem allow for high-throughput and data-heavy use cases over the gRPC API that were not possible before.

### New streaming gRPC endpoints for the Data and Policy APIs

The Data and Policy gRPC services now provide bidirectional streaming endpoints!
These endpoints work similarly to the experimental `BulkRW` endpoint that was explored in earlier versions of Enterprise OPA, and provide several speed and efficiency benefits over the REST API or existing unary gRPC endpoints.

They provide fixed-structure transactions that describe batches of CRUD operations, where all "write" operations (that would cause changes to the Data and Policy stores) are run as a single, sequential write transaction first, and then all "read" operations, such as rule queries, are evaluated in parallel.
If any write operations fail, the entire request fails.
Read operations have their failures reported as normal response messages with error-wrapping message types.

These batched operations can provide substantial throughput improvements over using the existing unary gRPC endpoints:
 - The connection cost is paid once at the start of the stream, instead of once per call.
 - Read and write operations enjoy greatly reduced contention for access to the Data and Policy stores.
 - Some operations are able to be parallelized.

For styles of API access where the successes and failures of write operations should be independent of one another, callers can send several smaller messages over the stream, and will receive back individual successful responses, or error messages for each failure that occurs.

See the [Enterprise OPA gRPC docs](https://buf.build/styra/enterprise-opa) on the Buf Schema Registry for more details.

## v1.5.1

This release fixes a typographical error in the protobuf markdown introduced when renaming Enterprise OPA.

## v1.5.0

This release changes the name of Styra Load to Styra Enterprise OPA.

## v1.4.1

This release updates the OPA version used in Styra Load to
[v0.53.1](https://github.com/open-policy-agent/opa/releases/tag/v0.53.1).

## v1.4.0

This release features a long-sought-after new built-in: `sql.send`!

### `sql.send`: query databases during policy evaluation

The new builtin in Styra Load, `sql.send`, can be considered `http.send`'s relational cousin:
It allows you to run any kind of custom query against a relational database management system, including

1. PostgreSQL
2. MySQL
3. SQLite

This is an example call, querying a SQLite database with a parametrized SQL query:
```rego
subordinate := sql.send({
	"driver": "sqlite",
	"data_source_name": "sqlite://data/company.db",
	"query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
	"args": [input.user, username],
})
count(subordinate.rows) > 0 # Make sure the row exists in the subordinates table.
```

Just like `http.send`, it allows you to pull in the most recent data you have in your database
when it's relevant for your policy decision.

[Find out more in the new tutorial.](https://docs.styra.com/load/tutorials/abac-with-sql)

### Export decision logs to S3

You can now send decision logs to S3-compatible stores.

[Find out more in the new tutorial.](https://docs.styra.com/load/tutorials/decision-logs/s3)

### CLI-based trial sign up

It is now possible to sign up for a free trial directly through the Styra Load CLI.
Running `load license trial` will collect all required information and generate a
new license key, which can be used to activate Styra Load immediately.

### OPA v0.53.0

This release includes OPA v0.53.0. [See the release notes for details.](https://github.com/open-policy-agent/opa/releases/tag/v0.53.0)

## v1.3.0

This release unveils two new feature sets, and includes some smaller quality-of-life improvements:

### External Key Management (EKM) via Hashicorp Vault

The Styra Load Vault integration can be used to:

* Retrieve the Styra Load License key from a Vault secret
* Override the configuration for a Styra Load service or key configuration
* Override the configuration of `http.send`

[See the documentation for more details.](https://docs.styra.com/load/configuration/using-secrets/from-hashicorp-vault)

### Enhanced Decision Logging

Styra Load now features its own decision logging infrastructure!
It gives you extra flexibility, and a multitude of new sinks, including
**Apache Kafka** and **Splunk**.

[Find out more about this by following the tutorial.](https://docs.styra.com/load/tutorials/decision-logs)

### More Enhancements

* The [Git data source](https://docs.styra.com/load/configuration/data/git) now allows configuring a `branch`.
* The [S3 data source](https://docs.styra.com/load/configuration/data/s3) now lets you provide an `endpoint`.
   This enables you to work with other S3-compatible APIs, like MinIO.


## v1.2.0

This release contains an update to the [latest version of OPA][gh-opa-v52] (`v0.52.0`), as well as bugfixes and performance improvements.

### CLI

 - LIA: Output now displays time values in human-friendly units, instead of always nanoseconds.

### Runtime

 - Small performance improvements around internal string caching.

### Fixes

 - Improved logging around licensing errors.
 - `data`: Plugin now detects and errors when a [bundle's roots][opa-bundle-format] would clash with the namespace owned by a [`data` plugin][load-data-plugins-reference].

   [gh-opa-v52]: https://github.com/open-policy-agent/opa/releases/tag/v0.52.0
   [opa-bundle-format]: https://www.openpolicyagent.org/docs/latest/management-bundles/#bundle-file-format
   [load-data-plugins-reference]: https://docs.styra.com/load/configuration/data/


## v1.1.0

This release includes a host of runtime performance improvements, bugfixes, and a new gRPC plugin.
Startup times have also been dramatically improved over older releases, thanks to upstream fixes in some of our dependencies.

### New protocol support via the `grpc` plugin

Load now supports gRPC versions of OPA's [Policy][opa-rest-policy] and [Data][opa-rest-data] [REST APIs][opa-rest-docs], as well as a new experimental bulk operations API.
The gRPC server is enabled via the `grpc` plugin.

   [opa-rest-policy]: https://www.openpolicyagent.org/docs/latest/rest-api/#policy-api
   [opa-rest-data]: https://www.openpolicyagent.org/docs/latest/rest-api/#data-api
   [opa-rest-docs]: https://www.openpolicyagent.org/docs/latest/rest-api/

The plugin can be enabled in your Load config file like so:

```yaml
plugins:
  grpc:
    addr: ":9090"
```

Or if you prefer the CLI, try: `load run -s --set plugins.grpc.addr=:9090`

In addition to the normal Load HTTP server, this will start up an unsecured gRPC server on the port you specified in the plugin's options.
This mode is great for testing with tools like [grpcurl][grpcurl], but we strongly recommend that you protect your gRPC server using one of the TLS options detailed below if you intend to make the gRPC port visible to other systems.

   [grpcurl]: https://github.com/fullstorydev/grpcurl

#### TLS Support

To secure the gRPC server, server-side TLS support is available.
Given the files `cert.pem` and `key.pem`, you could configure your Load instance to secure your gRPC connections like so:

```yaml
plugins:
  grpc:
    addr: ":9090"
    tls:
      cert_file: "cert.pem"
      cert_key_file: "key.pem"
```

#### mTLS Support

For additional security, mutual TLS (mTLS) connections can be used, where the client must present a certificate signed by the same Root CA as the server's certificate.
Given the root CA file `ca.pem`, we can add on to the configuration example for server-side TLS, and require clients to authenticate themselves using mTLS:

```yaml
plugins:
  grpc:
    addr: ":9090"
    authentication: "tls"
    tls:
      cert_file: "cert.pem"
      cert_key_file: "key.pem"
      ca_cert_file: "ca.pem"
```

Any client whose certificate was signed with `ca.pem` will be able to authenticate to the server.
All others will get disconnections or TLS errors.

### Runtime

 - Improved iteration speeds over large Rego Object types.
 - Improved memory efficiency via interning for some types.

### Fixes

 - Fixed a minor Rego incompatibility to match OPA's behavior.

## v1.0.1

* Performance improvements for queries of "all of `data`", like `load eval [...] data` or
  `GET /v1/data` with Load's API.
* Fix bug when referencing a bundle via `load eval bundle.tar.gz` (without explicitly loading it
  as a bundle via `-b`). This ensures compatibility with how OPA operates in these circumstances.
* Restructure parts of the gRPC API to make it more resource-focussed.
* Change the exit code for license validation related errors from 2 to 3 -- to differentiate them
  from any other errors.

## v1.0.0

This release marks the first general availability release of Styra Load.
Load provides a number of improvements over open source OPA, including:

 - Optimizations (CPU/Memory use)
 - Datasource integrations
 - Live Impact Analysis

## v0.102.5

 * This release is a release engineering fix to sort out part of our gRPC documentation system.

## v0.102.4

* Fix `--disable-telemetry` being ignored for `load run --server`.
* Use `google.protobuf.Value` and `google.protobuf.Struct` in the gRPC API instead of raw JSON strings.
* Further performance improvements to the Rego VM and bundle loading.

## v0.102.3

* Fix `load bundle convert` regression

## v0.102.1, v0.102.2

These releases have been release engineering fixes to sort out MacOS binary signing
of published executables.

## v0.102.0

* `load eval` now has a CLI flag for changing the instruction limit.
* Various BJSON bundle loading issues have been identified and fixed.
* Data paths controlled by data plugins are now protected from manual
  updates via the API.
* `load version` has been revamped.
* Windows users may have a better CLI experience now, as a
  superfluous user information lookup has been removed.
* Further performance improvements to the Rego VM.
* Updated the internal OPA version to v0.50.2.
* Various other third-party dependency bumps.

## v0.101.1

 * Fixed a hang triggered by sending the gRPC `BulkRW` endpoint multiple blank messages in sequence.

## v0.101.0

* Updated the internal OPA version to v0.50.0.
  See the [OPA Release Notes](https://github.com/open-policy-agent/opa/releases/tag/v0.50.0) for details.
* Live Impact Analysis can now be used from the CLI: `load liactl record`. See `load liactl help record`.
* Performance improvements to the Rego VM.
* Capabilities: Load now includes OPA-compatible capabilities data.
* Build: Load container images now include SBOM data.
* Various other third-party dependency bumps.
