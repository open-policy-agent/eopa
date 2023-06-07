#!/bin/sh

# For generating static HTML docs, we currently are using:
#   https://github.com/pseudomuto/protoc-gen-doc

mkdir -p doc/

docker run --rm \
  -v $(pwd)/doc:/out \
  -v $(pwd):/protos \
  pseudomuto/protoc-gen-doc \
  -I/protos enterprise-opa/v1/*.proto \
  --doc_opt=html,index.html
