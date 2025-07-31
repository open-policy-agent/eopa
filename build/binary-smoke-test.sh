#!/usr/bin/env bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0

set -eo pipefail
EOPA_EXEC="$1"
TARGET="$2"

PATH_SEPARATOR="/"
if [[ $EOPA_EXEC == *".exe" ]]; then
    PATH_SEPARATOR="\\"
fi

github_actions_group() {
    local args="$*"
    echo "::group::$args"
    $args
    echo "::endgroup::"
}

eopa() {
    local args="$*"
    github_actions_group $EOPA_EXEC $args
}

cleanup() {
    rm -f o?.tar.gz
    rm -f .signatures.json
    rm -f public_key.pem private_key.pem
    rm -rf builddir
}

trap "cleanup" EXIT

# assert_contains checks if the actual string contains the expected string.
assert_contains() {
    local expected="$1"
    local actual="$2"
    if [[ "$actual" != *"$expected"* ]]; then
        echo "Expected '$expected' but got '$actual'"
        exit 1
    fi
}

eopa version
eopa license
eopa eval -t $TARGET 'time.now_ns()'
eopa eval --format pretty --bundle test/cli/smoke/golden-bundle.tar.gz --input test/cli/smoke/input.json data.test.result --fail
eopa exec --bundle test/cli/smoke/golden-bundle.tar.gz --decision test/result test/cli/smoke/input.json
eopa build --output o0.tar.gz test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | eopa eval --format pretty --bundle o0.tar.gz -I data.test.result --fail
eopa build --optimize 1 --output o1.tar.gz test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | eopa eval --format pretty --bundle o1.tar.gz -I data.test.result --fail
eopa build --optimize 2 --output o2.tar.gz  test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | eopa eval --format pretty --bundle o2.tar.gz -I data.test.result --fail

eopa parse test/cli/smoke/test.rego

# Tar paths 
eopa build --output o3.tar.gz test/cli/smoke
eopa eval --bundle o3.tar.gz --input test/cli/smoke/input.json data.test.foo.bar -fpretty --fail
eopa bundle convert o3.tar.gz o9.tar.gz
eopa test --bundle o9.tar.gz -fpretty -v
github_actions_group assert_contains '/test/cli/smoke/test.rego' "$(tar -tf o3.tar.gz /test/cli/smoke/test.rego)"

# Verify eopa bjson
eopa bundle convert test/cli/smoke/golden-bundle.tar.gz o4.tar.gz
eopa eval --bundle o4.tar.gz --input test/cli/smoke/input.json data.test.result --fail

eopa exec --bundle o4.tar.gz --decision test/result test/cli/smoke/input.json
eopa check -b o4.tar.gz
eopa deps -b o4.tar.gz data
eopa inspect -a o4.tar.gz
eopa fmt -d o4.tar.gz
eopa bench -b o4.tar.gz data --metrics

# Verify sign/validation
echo "::group:: sign/verification"
openssl genpkey -algorithm RSA -out private_key.pem -pkeyopt rsa_keygen_bits:2048
openssl rsa -pubout -in private_key.pem -out public_key.pem

mkdir -p builddir
pushd builddir
tar xzf ../o4.tar.gz
popd

$EOPA_EXEC sign --signing-key private_key.pem --bundle builddir/
cp .signatures.json builddir/.

$EOPA_EXEC build --bundle --signing-key private_key.pem --verification-key public_key.pem builddir/ -o o5.tar.gz

$EOPA_EXEC eval --bundle o5.tar.gz --input test/cli/smoke/input.json data.test.result --fail

$EOPA_EXEC run -s --addr ":8183" -b o5.tar.gz --verification-key=public_key.pem &
last_pid=$!
sleep 2
curl --connect-timeout 10 --retry-connrefused --retry 3 --retry-delay 1 -X GET localhost:8183/v1/data
kill $last_pid
wait
echo "::endgroup::"

# Data files - correct namespaces
echo "::group:: Data files - correct namespaces"
assert_contains "data.namespace | test${PATH_SEPARATOR}cli${PATH_SEPARATOR}smoke${PATH_SEPARATOR}namespace${PATH_SEPARATOR}data.json" "$(eopa inspect test/cli/smoke)"
echo "::endgroup::"
