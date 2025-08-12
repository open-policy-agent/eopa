---
sidebar_position: 1
sidebar_label: Using Bundles
title: Data Bundle API | EOPA
---

:::note
See [Bundle Format](/eopa/explanation/bundle-format) for additional information.
:::


# Data Bundle API

Working in much the same way as in OPA, the Bundle API is a functionality of EOPA which can can periodically download EOPA formatted bundles from remote HTTP servers.

The following is an example of a simple EOPA configuration using the Bundle API, note that it's the same as how policy can is fetched:

```yaml
services:
  acmecorp:
    url: https://example.com/service/v1
    credentials:
      bearer:
        token: "bGFza2RqZmxha3NkamZsa2Fqc2Rsa2ZqYWtsc2RqZmtramRmYWxkc2tm"

bundles:
  authz:
    service: acmecorp
    resource: somedir/bundle.tar.gz
    persist: true
    polling:
      min_delay_seconds: 10
      max_delay_seconds: 20
```

Using this configuration, EOPA will fetch bundles from `https://example.com/service/v1/somedir/bundle.tar.gz` using a Bearer token for authentication.

Other advanced features of the Bundle API are explained in detail in the OPA documentation:

- [HTTP Long Polling](https://www.openpolicyagent.org/docs/management-bundles/#http-long-polling) for realtime updates
- [Loading multiple bundles](https://www.openpolicyagent.org/docs/management-bundles/#multiple-sources-of-policy-and-data)
- [Signed bundles](https://www.openpolicyagent.org/docs/management-bundles/#signing)
- [Supported public implementations](https://www.openpolicyagent.org/docs/management-bundles/#implementations)


## Datasource Plugins and Bundles

When using Datasource plugins (like [Kafka](/eopa/reference/configuration/data/kafka) or [HTTP](/eopa/reference/configuration/data/http)),
their respective configured data subtrees are protected in the same way that a bundle's roots
are protected: writes to the data path using the REST API will be forbidden.

This also applies when using **bundles and data plugins**: their respective roots needs to be
disjoint.
So any bundle used together with data plugins **must have a `.manifest`** file that declares its
roots.

```yaml
{
  "roots": ["transform"]
}
```
This manifest would be valid for a bundle that declares a message transform in `package transform`.
As long as none of the configured data plugins use the root `data.transform`, there will be no
conflicts.

If a bundle does **not declare roots**, it owns all of `data`, and will thus be in conflict with
**any data plugin**: if `data` cannot be modified, the data plugin is prohibited from writing to
any data subtree.
