#!/bin/bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0

# Runs in the current working directory, and generates a Github Actions job matrix.

# Parameter expansion + ternary trick for POSIX shell env var set guards:
# https://stackoverflow.com/a/307735
: "${TAGS:?Environment variable TAGS is not set or empty}"

# Find all directories containing Go test files.
PACKAGE_BASE=$(go list -tags=$TAGS -f '{{.Module.Path}}' ./... | uniq)
PACKAGES=$(go list -tags=$TAGS -f '{{ if  gt (len .TestGoFiles) 0 }}{{.ImportPath}}{{end}}' ./... | sed "s|^${PACKAGE_BASE}/||" | sort)
PACKAGES_JSON=$(echo "$PACKAGES" | jq -R -s -c 'split("\n") | map(select(length > 0))')
NUM_JOBS=$(echo "$PACKAGES" | grep -v '^$' | wc -l)

# Print discovered packages for debugging
echo "::group::Discovered Go Test Packages"
echo "$PACKAGES"
echo "::endgroup::"

# Check if we're exceeding GitHub's matrix limit
if [ "$NUM_JOBS" -ge 256 ]; then
  echo "::error::Matrix would generate $NUM_JOBS jobs. Github only allows 256 jobs per matrix run."
  echo "::error::See: https://docs.github.com/en/actions/administering-github-actions/usage-limits-billing-and-administration#usage-limits"
  exit 1
fi

echo "matrix=${PACKAGES_JSON}" >> $GITHUB_OUTPUT