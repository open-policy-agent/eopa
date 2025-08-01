---
sidebar_position: 11
sidebar_label: Performance Testing
title: Testing policy performance for RBAC use cases | EOPA
---

# Testing policy performance for RBAC use cases

EOPA is designed to be more performant in use cases where a large volume of data is needed to make a policy decision.
The following tutorial outlines the recommended way to make performance comparisons between EOPA and OPA.
Once you have completed this tutorial, you should have the tools you need to run follow on comparisons with your own policies and data.


## Example Domain: RBAC

This tutorial is based on a Role Based Access Control (RBAC) domain.
In the example domain, there are a number of users with various roles:

```json
{
  "user0": [
    "role5839", "role11814", "role13989" ...
  ],
  "user1": [
    "role5839", "role11814", "role13989" ...
  ]
}
```

Each of these roles grant rights to perform actions on resources:

```json
{
  "role0": [
    {
      "action": "action5996",
      "resource": "resource5579"
    },
    {
      "action": "action1170",
      "resource": "resource8171"
    },
    ...
  ],
  "role1": [
    {
      "action": "action7459",
      "resource": "resource1688"
    },
    ...
  ]
}
```

You can compare the performance of EOPA and OPA by processing this simple policy. This policy takes the `user` and checks if any of their `roles` permit the given `action` on the requested resource.

```rego
package rbac

import rego.v1

default allow := false

allow {
	some role in data.users[input.user]
	some permission in data.roles[role]
	permission.action == input.action
	permission.resource == input.resource
}
```

An example query with input for this policy is as follows:

```json
{
  "input": {
    "user": "user8147",
    "action": "action1258",
    "resource": "resource5952"
  },
  "expected": true # this is the expected response
}
```


## Prerequisites and Setup

There are a number of tools and resources needed to complete this exercise. This section how to configure performance testing.


### Tools

Performance tests can run on Linux, macOS, or Windows.

The following prerequisites are required for performance testing:

- EOPA as a binary, [Installation](/enterprise-opa/how-to/install/local) provides installation instructions.
- The latest OPA binary, [OPA Releases](https://github.com/open-policy-agent/opa/releases) provides installation instructions.
- The k6 benchmarking tool, [k6 Install](https://k6.io/docs/get-started/installation/) provides installation instructions.
- Git Large File Storage (to use pre-built bundles), [Git Large File Storage](https://git-lfs.com/) provides installation instructions.

Confirm that the `EOPA_LICENSE_KEY` environment variable is set in every terminal that will run EOPA.

Check the binaries are present in your path:

```shell
# terminal-command
eopa version
Version: ...

# terminal-command
opa version
Version: ...

# terminal-command
k6 version
k6 ...

# terminal-command
git lfs install
Updated Git hooks.
Git LFS initialized.

# terminal-command
git lfs checkout
Checking out LFS objects: 100% (10/10), 335 MB | 0 B/s, done.
```


### Resources

To download the resources for EOPA performance testing, clone the GitHub repository containing the examples:

```shell
# terminal-command
git clone https://github.com/StyraInc/enterprise-opa.git
# terminal-command
cd enterprise-opa/examples/performance-testing
```

We are going to be using some sample data which as been generated based on the example domain outlined above.

There are five sets of sample data ranging from 10 MB to 400 MB when uncompressed. For each set, there is a bundle for OPA and a bundle for EOPA. Also included are sample query sets which will be used to exercise the bundles during the test. Since bundles are compressed, the combined size of all downloads is around 335 MB.

Each dataset is based on the example domain above, only in varying sizes:

- **10 MB**: 12,000 users, 15,000 roles
- **50 MB**: 65,000 users and roles
- **100 MB**: 125,000 users and roles
- **200 MB**: 200,000 users and 280,000 roles
- **400 MB**: 500,000 users and roles


### Running Tests

The `benchmark.sh` script runs a performance test against OPA and then the same test against EOPA.
Supply the filename of the query list and the OPA and EOPA Bundles.

```shell
# terminal-command
./benchmark.sh
Usage: benchmark.sh [opa-bundle] [enterprise-opa-bundle] [query_list]
```

Start a test using the 400 MB dataset using the following:

```shell
# terminal-command
./benchmark.sh bundle-opa-400.tar.gz bundle-enterprise-opa-400.tar.gz queries-400
```

The test will take some time to run.


### Understanding Results

The results of a test run will look something like this, depending on your hardware:

```yaml
opa version: 0.48.0
eopa version: 0.48.0-1
k6 version: v0.42.0
OPA bundle: bundle-opa-400.tar.gz
EOPA bundle: bundle-enterprise-opa-400.tar.gz
Query list: queries-400

Waiting for OPA to start...
Running OPA test...
Results:
  requests per second (mean):   7851.13
  server heap size (max):       7.26GB
Stopping OPA...

Waiting for EOPA to start...
Running EOPA test...
Results:
  requests per second (mean):   10961.95
  server heap size (max):       1.12GB
Stopping EOPA...
```

You will see that the following statistics are reported for each test:

- **requests per second (mean)**: The average number of requests per second the server processed during the test.
- **server heap size (max)**: The maximum size of the heap during the test. This metric is sampled for 10% of requests. It makes sense to compare the maximum value for this metric since that's what you're going to need to provision for when running in production.


### Generating Your Own Sample Data

We also provide tools to generate your own data based on your own parameters. Take a look at the `generate-config.json` file:

```json
{
  "users": 100000,
  "roles": 5,
  "resources": 100000,
  "actions": 3,
  "queries": 1000,
  "max_capabilities_per_role": 10,
  "max_roles_per_user": 10
}
```

It should be intuitive how this can be used to generate a new dataset with different parameters. To so that, run the generate script:

```shell
# terminal-command
./generate.sh
```

:::note
This can take some time (minutes) if you have specified a large number of objects.
:::

This will output a `queries` file and a bundle file: `bundle.tar.gz`, you can convert this for use in EOPA with:

```shell
# terminal-command
eopa bundle convert bundle.tar.gz bundle-enterprise-opa.tar.gz
```

You can then run the tests again using these files to make your own comparisons.

```shell
# terminal-command
./benchmark.sh bundle.tar.gz bundle-enterprise-opa.tar.gz queries
```
