---
sidebar_label: Introduction
slug: /
title: Enterprise OPA Introduction
description: Enterprise OPA is an enterprise-grade drop-in replacement for Open Policy Agent (OPA)
sidebar_position: 0
---

<!-- markdownlint-disable MD044 -->
import EnterpriseOPAIntro from './_enterprise-opa-introduction.md';


# Enterprise OPA Introduction

<EnterpriseOPAIntro />

![Hello World](./helloworld.gif)


## See the performance improvements for yourself

The following commands are used to try Enterprise OPA.

1. `brew install styrainc/packages/eopa`
1. `eopa license trial`
1. `export EOPA_LICENSE_KEY=<your license key>`
1. `eopa run -s https://dl.styra.com/enterprise-opa/bundle-enterprise-opa-400.tar.gz`
1. `curl 'http://localhost:8181/metrics/alloc_bytes?pretty=true'`

The following commands compare Enterprise OPA with OPA.

1. `opa run -s -a localhost:8282 https://dl.styra.com/enterprise-opa/bundle-opa-400.tar.gz`
1. `curl 'http://localhost:8282/metrics/alloc_bytes?pretty=true'`

:::note
Memory usage for both Enterprise OPA and OPA peaks at launch. Wait a short time before checking the metrics to see typical operational figures.
:::


## Next steps

- Learn how to [install Enterprise OPA](/enterprise-opa/how-to/install)
- Follow one of our [tutorials](/enterprise-opa/tutorials)
- Learn how to [migrate from OPA](/enterprise-opa/how-to/migrate-from-opa).
