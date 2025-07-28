---
sidebar_position: 2
sidebar_label: Go with gRPC
title: Integrating in Go with gRPC | Enterprise OPA
---

<!-- markdownlint-disable MD044 -->
import LicenseTrialAdmonition from '../../../enterprise-opa/_license-trial-admonition.md';


# Integrating in Go with gRPC

Enterprise OPA offers a gRPC API for low-latency communication between production systems.
This API can be accessed during development with tools like `grpcurl`, but in production, it will almost always be accessed using code generated for a particular programming language.

:::info
Refer to the [gRPC API reference documentation](../../reference/api-reference/grpc-api) for the full usage options.
:::


## Overview

In this tutorial we'll be walking through how to use `buf` to generate Go client bindings for the Enterprise OPA gRPC API, and then dynamically update and query live data from Enterprise OPA over gRPC.
To demo that functionality, we'll need to complete the following:

- Generate the [Enterprise OPA gRPC API](https://github.com/StyraInc/enterprise-opa/tree/main/proto) client bindings.
- Create the policy and configuration data for the example.
- Create the Go client program.
- Run the client program and Enterprise OPA locally.


## Project setup

:::note
If you have an existing Golang project that you are integrating the Enterprise OPA gRPC definitions into, you can skip to the next step.
:::

Before we get started, we will need to do some housekeeping for the Go tooling.

If we are starting from scratch, however, we will need to create a new Go module before we can begin setting up the gRPC client bindings.
This allows the Go tooling to properly integrate the bindings into the project, as shown in the next step.

To create a new Go project, create a folder, and then run the following command, customizing it for your organization and repository name:

```shell
# terminal-command
go mod init github.com/{your_org_name}/{your_project_name}
```

:::info Example naming
For the purposes of the rest of this example, we're going to pretend we're creating a project at `github.com/hooli/mvp`
:::

This will create two files for Go's tooling, `go.mod`.
After running a command like `go mod tidy` you may also see a `go.sum` file.
Most of the Go language and editor tooling will automatically work with these files to keep your dependencies up to date as we move through the tutorial.


## Generating the gRPC Go client bindings for Enterprise OPA

To build the client bindings we will need for the demo application, we will need to have the following tooling installed and usable:

- The Protobuf compiler, `protoc`: [Platform-specific installation instructions](https://grpc.io/docs/protoc-installation/)
- Go plugins for the `protoc` compiler: [gRPC Quickstart guide for Go](https://grpc.io/docs/languages/go/quickstart/#prerequisites)
- Buf (used for `buf generate` later): [Installation guide for Buf](https://buf.build/docs/installation/)

We will also need to pull down the [`StyraInc/enterprise-opa` repository](https://github.com/StyraInc/enterprise-opa) with Git:

```shell
# terminal-command
git clone https://github.com/StyraInc/enterprise-opa && cp enterprise-opa/proto proto/
```

This will clone the Enterprise OPA blueprints repository down, and copy out the protobuf definitions folder for use in our project.
We can then update the Buf generation config to match our project's module name.

In the file `proto/buf.gen.yaml`, change the contents to match the following (customizing the organization and project names to match your project):

```yaml
---
version: v1
managed:
  enabled: true
  go_package_prefix:
    default: github.com/hooli/mvp # Customize
plugins:
  - plugin: go
    out: gen/go
    opt: paths=source_relative
  - plugin: go-grpc
    out: gen/go
    opt:
      - paths=source_relative
```

We can then run `buf generate` in the `proto/` folder.

This should create a folder named `proto/gen/go`, which will contain the Go bindings.


## Creating the `grpcpush` Go program

To demonstrate live updating of policies and data, we will create a Go program that will use the gRPC API to set up and run queries against the PetStore RBAC example from the Rego Playground.

The program will make three types of gRPC API calls at runtime:

- [`CreatePolicy`](https://buf.build/styra/enterprise-opa/docs/main:eopa.policy.v1#eopa.policy.v1.PolicyService.CreatePolicy): This call will upload the RBAC policy for the example.
- [`CreateData`](https://buf.build/styra/enterprise-opa/docs/main:eopa.data.v1#eopa.data.v1.DataService.CreateData): This call will upload the RBAC configuration dataset for the policy to use.
- [`GetData`](https://buf.build/styra/enterprise-opa/docs/main:eopa.data.v1#eopa.data.v1.DataService.GetData): These calls will be used to query the `/allow` rule from the policy.

Create a file called `main.go` in the current directory, and insert the following Go code shown below:

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	datav1 "github.com/hooli/mvp/proto/gen/go/load/data/v1" // Customize
	policyv1 "github.com/hooli/mvp/proto/gen/go/load/policy/v1" // Customize

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// Utility function to convert a JSON Object from text into a Protobuf struct quickly.
func bytesToProtoStruct(source []byte) (*structpb.Struct, error) {
	var temp map[string]interface{}
	if err := json.Unmarshal(source, &temp); err != nil {
		return nil, err
	}
	return structpb.NewStruct(temp)
}

func main() {
	ctx := context.Background()
	addr := flag.String("addr", "localhost:9090", "Address of the Enterprise OPA gRPC server.")
	dataFilename := flag.String("datafile", "data.json", "Name of the config data JSON file to use.")
	policyFilename := flag.String("policyfile", "policy.rego", "Name of the Rego policy file to use.")

	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Printf(`Error: QUERY argument required.

Usage:
  grpcpush [OPTIONS] '/path/to/my/rule'

Example: (Assuming a rule at 'app.rbac.allow')
  grpcpush '/app/rbac/allow'
`)
		os.Exit(1)
	}
	query := args[0]

	// Connect to the Enterprise OPA instance.
	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial the Enterprise OPA server: %v", err)
	}
	defer conn.Close()
	clientData := datav1.NewDataServiceClient(conn)
	clientPolicy := policyv1.NewPolicyServiceClient(conn)

	// Read in and push the JSON config data to the Enterprise OPA instance over gRPC.
	configData, err := os.ReadFile(*dataFilename)
	if err != nil {
		log.Fatal(err)
	}
	configStruct, err := bytesToProtoStruct(configData)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := clientData.CreateData(ctx, &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/", Document: structpb.NewStructValue(configStruct)}}); err != nil {
		log.Fatalf("CreateData failed: %v", err)
	}

	// Create a new policy by reading the policy file in, and then pushing the policy up to the Enterprise OPA instance over gRPC.
	policy, err := os.ReadFile(*policyFilename)
	if err != nil {
		log.Fatal(err)
	}
	_, err = clientPolicy.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "example", Text: string(policy)}})
	if err != nil {
		log.Fatalf("CreatePolicy failed: %v", err)
	}

	// Read in query data from stdin.
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if inputStruct, err := bytesToProtoStruct(scanner.Bytes()); err == nil {
			doc := &datav1.InputDocument{Document: inputStruct}
			resp, err := clientData.GetData(ctx, &datav1.GetDataRequest{Path: query, Input: doc})
			if err != nil {
				log.Fatalf("GetData failed: %v", err)
			}
			resultDoc := resp.GetResult()
			path := resultDoc.GetPath()
			data := resultDoc.GetDocument()
			fmt.Println(path, data.GetBoolValue())
		} else {
			log.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading standard input:", err)
	}
}
```

We can then build this program for local testing with the command:
```shell
# terminal-command
go build -o grpcpush main.go
```


## Creating the demo policy

We are going to borrow the RBAC PetStore example from the [Rego Playground][rego-playground] for this tutorial.

Create a file named `policy.rego` with the following contents:

```rego
# Role-based Access Control (RBAC)
# --------------------------------
#
# This example defines an RBAC model for a Pet Store API. The Pet Store API allows
# users to look at pets, adopt them, update their stats, and so on. The policy
# controls which users can perform actions on which resources. The policy implements
# a classic Role-based Access Control model where users are assigned to roles and
# roles are granted the ability to perform some action(s) on some type of resource.
#

package app.rbac

import rego.v1

# By default, deny requests.
default allow := false

# Allow admins to do anything.
allow if user_is_admin

# Allow the action if the user is granted permission to perform the action.
allow := matches(grant) if some grant in user_is_granted

matches(grant) if {
	input.action == grant.action
	input.type == grant.type
}

# user_is_admin is true if "admin" is among the user's roles as per data.user_roles
user_is_admin if "admin" in data.user_roles[input.user]

# user_is_granted is a set of grants for the user identified in the request.
# The `grant` will be contained if the set `user_is_granted` for every...
user_is_granted contains grant if {
	# `role` assigned an element of the user_roles for this user...
	some role in data.user_roles[input.user]

	# `grant` assigned a single grant from the grants list for 'role'...
	some grant in data.role_grants[role]
}
```

This Rego code will check queries to the `/allow` rule against the RBAC configuration stored under `data.user_roles` and `data.role_grants`.
We will create that configuration dataset in the next step.


## Creating the RBAC configuration data

We will borrow from the RBAC PetStore example again for our JSON config data.

Create a file named `data.json` with the following contents:

```json
{
  "user_roles": {
    "alice": [
      "admin"
    ],
    "bob": [
      "employee",
      "billing"
    ],
    "eve": [
      "customer"
    ]
  },
  "role_grants": {
    "customer": [
      {
        "action": "read",
        "type": "dog"
      },
      {
        "action": "read",
        "type": "cat"
      },
      {
        "action": "adopt",
        "type": "dog"
      },
      {
        "action": "adopt",
        "type": "cat"
      }
    ],
    "employee": [
      {
        "action": "read",
        "type": "dog"
      },
      {
        "action": "read",
        "type": "cat"
      },
      {
        "action": "update",
        "type": "dog"
      },
      {
        "action": "update",
        "type": "cat"
      }
    ],
    "billing": [
      {
        "action": "read",
        "type": "finance"
      },
      {
        "action": "update",
        "type": "finance"
      }
    ]
  }
}
```

This sample data will be used by the RBAC policy we created in a previous step.


## Running `grpcpush` and Enterprise OPA together

Now that we have all of our setup work out of the way, we can finally run Enterprise OPA and the demo program together.


### Configuring and running Enterprise OPA locally

Create a file called `enterprise-opa-conf.yaml` and insert the YAML configuration below.

```yaml
plugins:
  grpc:
    addr: "127.0.0.1:9090"
```

Before running Enterprise OPA, we will need to set the `EOPA_LICENSE_KEY` environment variable.

<LicenseTrialAdmonition />


```shell
# terminal-command
export EOPA_LICENSE_KEY=<license key here>
```

We can now run Enterprise OPA in server mode, with the `grpc` plugin enabled:

```shell
# terminal-command
eopa run --server --config enterprise-opa-conf.yaml
```

This will start up Enterprise OPA, and will provide an unsecured gRPC server at the address `localhost:9090`.


### Running the `grpcpush` program

In a previous step, we created a Go program that consumes input data from standard input, and then sends [`GetData`](https://buf.build/styra/enterprise-ops/docs/main:eopa.data.v1#eopa.data.v1.DataService.GetData) gRPC requests using that data.

To test several queries with differing input data against the PetStore RBAC example, we will create a file with one JSON object per line, named `input.jsonl`.

Create `input.jsonl` with the following contents:

```json
{
  "user": "alice",
  "action": "read",
  "object": "id123",
  "type": "dog"
}
{
  "user": "bob",
  "action": "read",
  "object": "id123",
  "type": "dog"
}
{
  "user": "eve",
  "action": "read",
  "object": "id123",
  "type": "dog"
}
{
  "user": "alice",
  "action": "update",
  "object": "id123",
  "type": "dog"
}
{
  "user": "bob",
  "action": "update",
  "object": "id123",
  "type": "dog"
}
{
  "user": "eve",
  "action": "update",
  "object": "id123",
  "type": "dog"
}
```

We can now run the `grpcpush` program like so:

```shell
# terminal-command
grpcpush '/app/rbac/allow' <input.jsonl
```

This should generate the following output on the command line:

```txt
/app/rbac/allow true
/app/rbac/allow true
/app/rbac/allow true
/app/rbac/allow true
/app/rbac/allow true
/app/rbac/allow false
```

We can see in the above example that the user `alice` with the `customer` role is allowed to perform the `read` action, but not the `update` action, which matches what we would expect for the policy and configuration data we have provided.

   [rego-playground]: https://play.openpolicyagent.org/
