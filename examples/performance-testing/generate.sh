#!/usr/bin/env bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0


set -eo pipefail

if command -v opa>/dev/null 2>&1 ; then
    echo "opa version: $(opa version | head -n 1 | cut -d ' ' -f 2)"
else
    echo "opa not found"
    exit 1
fi

echo "Generating test data with config:"

cat generate-config.json

bundle_path="$(PWD)/bundle"

rm -rf "$bundle_path"
mkdir -p "$bundle_path"
mkdir -p "$bundle_path/users"
mkdir -p "$bundle_path/roles"

temp_dir=$(mktemp -d)
data_file="$temp_dir"/data.json

opa eval -d generate.rego -i generate-config.json data.generate > "$data_file"

opa eval 'input.result[0].expressions[0].value.queries' -fraw -i "$data_file" > queries
opa eval 'input.result[0].expressions[0].value.users' -fraw -i "$data_file" > "$bundle_path/users/data.json"
opa eval 'input.result[0].expressions[0].value.roles' -fraw -i "$data_file" > "$bundle_path/roles/data.json"

echo "New bundle created at $bundle_path"
du -sh bundle

rm -r "$temp_dir"
