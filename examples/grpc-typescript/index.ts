/**
 * Copyright 2025 The OPA Authors
 * SPDX-License-Identifier: Apache-2.0
 */

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
