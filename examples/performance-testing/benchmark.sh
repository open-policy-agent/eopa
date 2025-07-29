#!/usr/bin/env bash

set -eo pipefail

TEST_DURATION=5s

opa_pid=""
eopa_pid=""

function cleanup {
  if [ ! -z "$opa_pid" ]; then
    kill "$opa_pid"
  fi
  if [ ! -z "$eopa_pid" ]; then
    kill "$eopa_pid"
  fi
}

trap cleanup EXIT
trap cleanup ERR

usage="$(basename "$0") [opa-bundle] [eopa-bundle] [query_list]"

if [ "$#" -ne 3 ]; then
    echo "Usage: $usage"
    exit 1
fi

if command -v opa>/dev/null 2>&1 ; then
    echo "opa version: $(opa version | head -n 1 | cut -d ' ' -f 2)"
else
    echo "opa not found"
    exit 1
fi

if command -v eopa>/dev/null 2>&1 ; then
    echo "eopa version: $(eopa version | head -n 1 | cut -d ' ' -f 2)"
else
    echo "eopa not found"
    exit 1
fi

if command -v k6>/dev/null 2>&1 ; then
    echo "k6 version: $(k6 version | head -n 1 | cut -d ' ' -f 2)"
else
    echo "k6 not found"
    exit 1
fi

if [ -z "${EOPA_LICENSE_KEY}" ]; then
    echo "EOPA_LICENSE_KEY must be set"
    exit 1
fi

echo "OPA bundle: $1"
echo "EOPA bundle: $2"
echo "Query list: $3"
echo ""

# OPA Test
nohup opa run --server -b "$1" > opa.log 2>&1 &
opa_pid="$!"

while [[ "$(curl -X "POST" -d $'{"input": {}}' -s -o /dev/null -w ''%{http_code}'' http://localhost:8181/v1/data/rbac/allow?metrics)" != "200" ]]; do
  echo "Waiting for OPA to start..."
  sleep 4
done
echo "Running OPA test..."

k6 -q run -u 10 -d $TEST_DURATION -e HOST=localhost -e QUERY_FILE="$3" test.js
echo "" # needed to make sure k6 output is on a new line
echo "Stopping OPA..."
kill "$opa_pid"
opa_pid=""

echo ""

# EPA Test
nohup eopa run --server -b "$2" > eopa.log 2>&1 &
eopa_pid="$!"

while [[ "$(curl -X "POST" -d $'{"input": {}}' -s -o /dev/null -w ''%{http_code}'' http://localhost:8181/v1/data/rbac/allow?metrics)" != "200" ]]; do
  echo "Waiting for EOPA to start..."
  sleep 2
done
echo "Running EOPA test..."

k6 -q run -u 10 -d $TEST_DURATION -e HOST=localhost -e QUERY_FILE="$3" test.js
echo "" # needed to make sure k6 output is on a new line
echo "Stopping EOPA..."
kill "$eopa_pid"
eopa_pid=""

cleanup
