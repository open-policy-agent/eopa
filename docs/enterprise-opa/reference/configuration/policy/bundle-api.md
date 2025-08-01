---
sidebar_position: 1
sidebar_label: Using Bundles
title: Policy Bundle API | EOPA
---

:::note
See [Bundle Format](/enterprise-opa/explanation/bundle-format) for additional information.
:::


# Policy Bundle API

Working in much the same way as in OPA, the Bundle API is a functionality of EOPA which can can periodically download bundles of policy from remote HTTP servers.

The following is an example of a simple EOPA configuration using the Bundle API:

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
- [Supported public implementations](https://www.openpolicyagent.org/docs/management-bundles/#implementations) for example, Amazon S3, Google Cloud Storage, and Azure Blob Storage)
