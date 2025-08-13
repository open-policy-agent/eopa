# EOPA gRPC API

This folder contains the definitions of [Protocol Buffers][protobuf] used by [EOPA][gh-eopa].

We use [Buf][buf] to manage and generate source code from the protocol buffer definitions.
The protobuf definitions here used to be pushed at release-time to a repository in the Buf Registry.

   [protobuf]: https://developers.google.com/protocol-buffers/
   [buf]: https://github.com/bufbuild/buf
   [gh-eopa]: https://github.com/open-policy-agent/eopa

## Build

Running `buf generate` in this folder (or `./buf.gen.yaml` if you're on a Linux system) should create the necessary Golang files under a folder named `gen/`.

For supporting other languages, you will need to modify the `buf.gen.yaml` file to add the appropriate generation arguments for your language of choice.

## Linting

To lint the protobuf files, try running `buf lint` in this folder.
