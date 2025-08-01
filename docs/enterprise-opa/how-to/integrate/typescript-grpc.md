---
sidebar_position: 3
sidebar_label: gRPC with Typescript
title: Using EOPA gRPC API from TypeScript
---


# Using gRPC from TypeScript

To use the [EOPA](/enterprise-opa) [gRPC API](/enterprise-opa/reference/api-reference/grpc-api) from TypeScript, we're relying on the SDKs generated from the Protobuf Schema published on [Buf](https://buf.build/styra/enterprise-opa).


## Installation

Add the registry to your NPM setup (only needs to be done once):

<Tabs groupId="pkg" queryString>
<TabItem value="npm" label="npm">

```shell
# terminal-command
npm config set @buf:registry https://buf.build/gen/npm/v1/
```

</TabItem>
<TabItem value="yarn" label="yarn">

```shell
# terminal-command
yarn config set npmScopes.buf.npmRegistryServer https://buf.build/gen/npm/v1/
```

</TabItem>
</Tabs>

Install the generated SDK:

<Tabs groupId="pkg" queryString>
<TabItem value="npm" label="npm">

```shell
# terminal-command
npm install @buf/styra_enterprise-opa.connectrpc_es@latest \
  @bufbuild/protobuf@^1.10.0 \
  @connectrpc/connect-node@^1.4.0
```

</TabItem>
<TabItem value="yarn" label="yarn">

```shell
# terminal-command
yarn add @buf/styra_enterprise-opa.connectrpc_es@latest \
  @bufbuild/protobuf@^1.10.0 \
  @connectrpc/connect-node@^1.4.0
```

</TabItem>
</Tabs>


## Usage Example

This code requests the equivalent of `POST /v1/data/my/policy` with input via gRPC:

```ts
import { createPromiseClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { Struct } from "@bufbuild/protobuf";

// generated code served from Buf Schema Registry
import { DataService } from "@buf/styra_enterprise-opa.connectrpc_es/eopa/data/v1/data_connect";
import {
  GetDataRequest,
  InputDocument,
} from "@buf/styra_enterprise-opa.bufbuild_es/eopa/data/v1/data_pb.js";

const transport = createGrpcTransport({
  baseUrl: "http://127.0.0.1:9090",
  httpVersion: "2",
});
const client = createPromiseClient(DataService, transport);

const input = Struct.fromJsonString(`{ "hello": "world" }`);
const req = new GetDataRequest({
  path: "/my/policy", // "data.my.policy"
  input: new InputDocument({ document: input }),
});

const resp = await client.getData(req);
console.log(resp.result?.document?.toJson()); // => { example: true }
```

Now run an EOPA instance serving this Rego,

```rego
# policy.rego
package my.policy

import rego.v1

example if input.hello == "world"
```

with a config enabling gRPC,

```yaml
# enterprise-opa.yml
plugins:
  grpc:
    addr: localhost:9090
```

via `eopa run --server --config-file enterprise-opa.yml policy.rego`, and execute the TypeScript code:

```shell
# terminal-command
node --import tsx index.ts
{ example: true }
```

For the full `package.json`, and the example configuration of EOPA, please see [the examples repository](https://github.com/StyraInc/enterprise-opa/tree/main/examples/grpc-typescript).


## References

- [Example code](https://github.com/StyraInc/enterprise-opa/tree/main/examples/grpc-typescript)
- [Buf Generated SDKs](https://buf.build/docs/bsr/generated-sdks/npm)
