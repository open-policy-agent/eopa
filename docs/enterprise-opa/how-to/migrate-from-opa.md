---
sidebar_position: 4
sidebar_label: Migrate from OPA
title: How to migrate from Open Policy Agent to Enterprise OPA
---

# Migrating to Enterprise OPA from OPA

1. [Convert bundles to Enterprise OPA format](#convert-bundles)
2. [Run with a license](#run-with-a-license)
3. [Validate your networking](#validate-networking)


## Convert bundles

Enterprise OPA uses a different [bundle format](/enterprise-opa/explanation/bundle-format). To convert a bundle from OPA format to Enterprise OPA format run the `eopa bundle convert` command:

```sh
# terminal-command
eopa bundle convert <input-bundle-location> <output-bundle-location>
```

:::danger
Discovery bundles should not be converted to the Enterprise OPA format.
:::

Refer to the [`eopa bundle convert` CLI reference](/enterprise-opa/reference/cli-reference#eopa-bundle-convert) for a full list of options


## Run with a license

Refer to the [How to run Enterprise OPA with a license guide](/enterprise-opa/how-to/run/with-a-license)


## Validate networking

For security reasons, Enterprise OPA only binds to `localhost:8181` vs. the default `:8181` in Open Policy Agent.

To modify the address where Enterprise OPA is available, specify the `--addr` flag when starting Enterprise OPA. e.g.

```shell
# terminal-command
eopa run -s --addr=":8181" ...
```

Refer to the [`eopa run` CLI reference](/enterprise-opa/reference/cli-reference#eopa-run) for a full list of options
