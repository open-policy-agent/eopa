#!/usr/bin/env bash
set -eo pipefail
LOAD_EXEC="$1"
TARGET="$2"

PATH_SEPARATOR="/"
if [[ $LOAD_EXEC == *".exe" ]]; then
    PATH_SEPARATOR="\\"
fi

github_actions_group() {
    local args="$*"
    echo "::group::$args"
    $args
    echo "::endgroup::"
}

load() {
    local args="$*"
    github_actions_group $LOAD_EXEC $args
}

# assert_contains checks if the actual string contains the expected string.
assert_contains() {
    local expected="$1"
    local actual="$2"
    if [[ "$actual" != *"$expected"* ]]; then
        echo "Expected '$expected' but got '$actual'"
        exit 1
    fi
}

load version
load eval -t $TARGET 'time.now_ns()'
load eval --format pretty --bundle test/cli/smoke/golden-bundle.tar.gz --input test/cli/smoke/input.json data.test.result --fail
load exec --bundle test/cli/smoke/golden-bundle.tar.gz --decision test/result test/cli/smoke/input.json
load build --output o0.tar.gz test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | load eval --format pretty --bundle o0.tar.gz -I data.test.result --fail
load build --optimize 1 --output o1.tar.gz test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | load eval --format pretty --bundle o1.tar.gz -I data.test.result --fail
load build --optimize 2 --output o2.tar.gz  test/cli/smoke/data.yaml test/cli/smoke/test.rego
echo '{"yay": "bar"}' | load eval --format pretty --bundle o2.tar.gz -I data.test.result --fail

# Tar paths 
load build --output o3.tar.gz test/cli/smoke
github_actions_group assert_contains '/test/cli/smoke/test.rego' "$(tar -tf o3.tar.gz /test/cli/smoke/test.rego)"

load bundle convert o3.tar.gz o4.tar.gz

load test -b o4.tar.gz

# load with plugins
$LOAD_EXEC run --config-file build/plugins.yaml -s &
last_pid=$!
sleep 1
curl --connect-timeout 10 --retry-connrefused --retry 3 --retry-delay 1 -X PUT localhost:8181/v1/policies/test -d 'package foo allow := x {x = true}'
curl --connect-timeout 10 --retry-connrefused --retry 3 --retry-delay 1 -X POST localhost:8181/v1/data/foo -d '{"input": {}}'
kill -9 $last_pid

# Data files - correct namespaces
echo "::group:: Data files - correct namespaces"
assert_contains "data.namespace | test${PATH_SEPARATOR}cli${PATH_SEPARATOR}smoke${PATH_SEPARATOR}namespace${PATH_SEPARATOR}data.json" "$(load inspect test/cli/smoke)"
echo "::endgroup::"

# Test server mode (requires a license): load run -s
$LOAD_EXEC run -s &
last_pid=$!
sleep 1
curl --connect-timeout 10 --retry-connrefused --retry 3 --retry-delay 1 -X PUT localhost:8181/v1/policies/test -d 'package foo allow := x {x = true}'
curl --connect-timeout 10 --retry-connrefused --retry 3 --retry-delay 1 -X POST localhost:8181/v1/data/foo -d '{"input": {}}'
kill $last_pid

rm -f o?.tar.gz
