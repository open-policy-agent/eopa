#!/bin/sh
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0


# For generating static HTML docs, we currently are using:
#   https://github.com/pseudomuto/protoc-gen-doc

mkdir -p doc/

docker run --rm \
  -v $(pwd)/doc:/out \
  -v $(pwd):/protos \
  pseudomuto/protoc-gen-doc \
  -I/protos enterprise-opa/v1/*.proto \
  --doc_opt=html,index.html
