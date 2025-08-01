---
sidebar_position: 2
sidebar_label: gRPC API
title: gRPC API | EOPA
---


# gRPC API

The gRPC API exposes endpoints via the gRPC protocol for managing data and policies.

:::tip
Executing a policy (i.e. querying for an authorization decision) is done by using the [GetData API](https://buf.build/styra/enterprise-opa/docs/main:eopa.data.v1#eopa.data.v1.DataService.GetData)
:::


## Bulk API

BulkService is an API for specifying batches of read/write operations.

See the [buf.build Bulk API reference documentation](https://buf.build/styra/enterprise-opa/docs/main:eopa.bulk.v1)


## Data API

DataService is an API for CRUD operations on the data stored in EOPA.

See the [buf.build Data API reference documentation](https://buf.build/styra/enterprise-opa/docs/main:eopa.data.v1)

The Data API also has the ability to [stream reads/writes](https://buf.build/styra/enterprise-opa/docs/main:eopa.data.v1#eopa.data.v1.DataService.StreamingDataRW)


## Policy API

PolicyService is an API for CRUD operations on the policies stored in EOPA.

See the [buf.build Policy API reference documentation](https://buf.build/styra/enterprise-opa/docs/main:eopa.policy.v1)

The Policy API also has the ability to [stream reads/writes](https://buf.build/styra/enterprise-opa/docs/main:eopa.policy.v1#eopa.policy.v1.PolicyService.StreamingPolicyRW)
