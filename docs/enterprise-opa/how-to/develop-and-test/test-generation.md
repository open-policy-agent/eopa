---
sidebar_position: 2
sidebar_label: Test Generation
title: How to generate test scaffolding
---

:::tip
You **do not need** an EOPA license to use this functionality.
:::


# How to generate tests and test scaffolding

This guide shows you how to use the EOPA CLI to generate new tests and
bootstrap an application with tests.

1. [Install the EOPA CLI](#install-the-enterprise-opa-cli)
1. [Run `eopa test bootstrap` to generate a starting set of test stubs (one test group per rule body)](#eopa-test-bootstrap)
1. [Use `eopa test new` with naming annotations to generate new test cases](#eopa-test-new)

---


## Install the EOPA CLI

```sh
# terminal-command
brew install styrainc/packages/eopa
```

See the [installation reference guide](/enterprise-opa/how-to/install) for alternatives.

---


## `eopa test bootstrap`

Given the file `example.rego`:
```rego
package example

import rego.v1

servers := ["dev", "canary", "prod"]

default allow := false

allow if {
	input.action == "fetch"
	input.servers.names[_] == data.servers[_]
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

Refer to the [`eopa test bootstrap` CLI reference](/enterprise-opa/reference/cli-reference#eopa-test-bootstrap) for a full list of options.

---


## `eopa test new`


If we add a new rule to the policy with an [OPA metadata annotation](https://www.openpolicyagent.org/docs/policy-language/#metadata) `test-bootstrap-name`:
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

Refer to the [`eopa test new` CLI reference](/enterprise-opa/reference/cli-reference#eopa-test-new) for a full list of options.
