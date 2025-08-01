---
sidebar_position: 3
sidebar_label: Kicking the tires with gRPC
title: Kicking the tires with gRPC | EOPA
---

# Kicking the tires with gRPC

EOPA has a gRPC API, allowing low-latency, efficient communication between production systems. It emulates the OPA v1 REST API endpoints, and includes a few experimental endpoints which can provide improved performance for some workloads.

The following resources provide additional information:

- [gRPC Reference Documentation](https://buf.build/styra/enterprise-opa)
- [OPA Rest API](https://www.openpolicyagent.org/docs/rest-api/)

:::info
Refer to the [gRPC API reference documentation](../../reference/api-reference/grpc-api) for the full usage options.
:::


## Prerequisites

This tutorial relies on the [`grpcurl`][grpcurl] tool. Binaries are available from the project's [GitHub Releases page](https://github.com/fullstorydev/grpcurl/releases).


## Configuration

The gRPC service is provided by the `grpc` plug-in, and needs to be enabled in EOPA's configuration before the gRPC endpoints are made available to the network.

`eopa-conf.yaml`

```yaml
plugins:
  grpc:
    addr: localhost:9090
```

The minimalist configuration above will enable the gRPC service, and bind it to local port 9090.


## Setup

We'll follow the [example from the OPA introductory tutorial][opa-intro-tutorial], but with a twist: all of the network steps will be done through gRPC.

We'll need two files first: a policy file and a data file.

`input.json`

```json
{
  "servers": [
    {
      "id": "app",
      "protocols": [
        "https",
        "ssh"
      ],
      "ports": [
        "p1",
        "p2",
        "p3"
      ]
    },
    {
      "id": "db",
      "protocols": [
        "mysql"
      ],
      "ports": [
        "p3"
      ]
    },
    {
      "id": "cache",
      "protocols": [
        "memcache"
      ],
      "ports": [
        "p3"
      ]
    },
    {
      "id": "ci",
      "protocols": [
        "http"
      ],
      "ports": [
        "p1",
        "p2"
      ]
    },
    {
      "id": "busybox",
      "protocols": [
        "telnet"
      ],
      "ports": [
        "p1"
      ]
    }
  ],
  "networks": [
    {
      "id": "net1",
      "public": false
    },
    {
      "id": "net2",
      "public": false
    },
    {
      "id": "net3",
      "public": true
    },
    {
      "id": "net4",
      "public": true
    }
  ],
  "ports": [
    {
      "id": "p1",
      "network": "net1"
    },
    {
      "id": "p2",
      "network": "net3"
    },
    {
      "id": "p3",
      "network": "net2"
    }
  ]
}
```

`example.rego`

```rego
package example

import rego.v1

default allow := false # unless otherwise defined, allow is false

allow if count(violation) == 0 # allow is true if there are zero violations.

violation[server.id] { # a server is in the violation set if...
	some server in public_server # it exists in the 'public_server' set and...
	"http" in server.protocols # it contains the insecure "http" protocol.
}

violation[server.id] { # a server is in the violation set if...
	some server in public_server # it exists in the input.servers collection and...
	"telnet" in server.protocols # it contains the "telnet" protocol.
}

public_server[server] { # a server exists in the public_server set if...
	some i, j
	server := input.servers[_] # it exists in the input.servers collection and...
	server.ports[_] == input.ports[i].id # it references a port in the input.ports collection and...
	input.ports[i].network == input.networks[j].id # the port references a network in the input.networks collection and...
	input.networks[j].public # the network is public.
}
```

Once we have these two files, we can then use [`grpcurl`][grpcurl] to follow the rest of the tutorial, although we'll need to alter most steps to work with the JSON formats that `grpcurl` expects to see.


## Starting up the EOPA server

To run EOPA with the configuration, run the command below:

```sh
# terminal-command
eopa run -s -c eopa-conf.yaml
```

The policy will be pushed to the server as part of the steps below, so we do not include it at startup.


## Creating request data

We'll end up making three gRPC calls against the EOPA server:

- [`eopa.policy.v1.PolicyService/CreatePolicy`](https://buf.build/styra/eopa/docs/main:eopa.policy.v1#eopa.policy.v1.PolicyService.CreatePolicy) :: Inserts the code from `example.rego` into the policy store.
- [`eopa.data.v1.DataService/GetData`](https://buf.build/styra/eopa/docs/main:eopa.data.v1#eopa.data.v1.DataService.GetData) :: Queries the `/example` document (running all rules in the policy).
- [`eopa.data.v1.DataService/GetData`](https://buf.build/styra/eopa/docs/main:eopa.data.v1#eopa.data.v1.DataService.GetData) :: Queries only the `/example/violation` rule.

Below is a Bash script that you can use to quickly generate the correct JSON request formats for `grpcurl`:

`create_testdata.sh`
```bash
#!/bin/bash

# Policies are sent as strings, and because we're using grpcurl, we
# have to escape the policy text so that it can be used as a JSON value.
json_escape () {
    python -c 'import json,sys; print(json.dumps(sys.stdin.read()))'
}

# Create a JSON-formatted request description for grpcurl, targeting the
# `eopa.policy.v1.PolicyService/CreatePolicy` endpoint.
cat <<EOF > v1-policy-input.json
{
  "policy": {
    "path": "/example",
    "text": $(json_escape < example.rego)
  }
}
EOF

# Create a JSON-formatted request description for grpcurl, targeting the
# `eopa.data.v1.DataService/GetData` endpoint.
cat <<EOF > v1-data-input-1.json
{
  "path": "/example/violation",
  "input": {
    "document": $(cat input.json)
  }
}
EOF

# Create a JSON-formatted request description for grpcurl, targeting the
# `eopa.data.v1.DataService/GetData` endpoint, using a different query path.
cat <<EOF > v1-data-input-2.json
{
  "path": "/example/allow",
  "input": {
    "document": $(cat input.json)
  }
}
EOF
```

After running `./create_testdata.sh`, we should have three JSON files available for quickly testing out the gRPC APIs.


## Making the gRPC API calls

We can then push up our policy, and query the two endpoints from the original tutorial, `example/violation` and `example/allow` as follows:

```bash
# Uploads a policy to the EOPA server.
# terminal-command
grpcurl -d @ -plaintext localhost:9090 eopa.policy.v1.PolicyService/CreatePolicy <v1-policy-input.json

# Queries the policy's 'example/violation' rule.
# terminal-command
grpcurl -d @ -plaintext localhost:9090 eopa.data.v1.DataService/GetData <v1-data-input-1.json

# Queries the policy's 'example/allow' rule.
# terminal-command
grpcurl -d @ -plaintext localhost:9090 eopa.data.v1.DataService/GetData <v1-data-input-2.json
```

The output from `grpcurl` will look like:

```json
{}
{
  "result": {
    "path": "/example/violation",
    "document": [
      "busybox",
      "ci"
    ]
  }
}
{
  "result": {
    "path": "/example/allow",
    "document": false
  }
}
```

The results should be the same as we'd expect from the OPA REST APIs.

   [opa-intro-tutorial]: https://www.openpolicyagent.org/docs/#3-try-opa-run-interactive
   [grpcurl]: https://github.com/fullstorydev/grpcurl
