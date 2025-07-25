#!/usr/bin/env bash
# Given a variable VERSION (e.g. from go list -m -f '{{.Version}}' github.com/open-policy-agent/opa)
# echoes the normalized OPA version according to the following rules:
# - If VERSION is "v1.6.0"         => output "1.6.0"
# - If VERSION is "v1.6.1"         => output "1.6.1"
# - If VERSION is "v1.6.1-..."     => output "1.6.0" (patch reduced by 1 if suffix present)

VERSION="$(go list -m -f '{{.Version}}' github.com/open-policy-agent/opa)"

# Remove leading "v" if present
ver="${VERSION#v}"

# Extract major, minor, patch, and rest (if any)
if [[ "$ver" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)(-.*)?$ ]]; then
    major="${BASH_REMATCH[1]}"
    minor="${BASH_REMATCH[2]}"
    patch="${BASH_REMATCH[3]}"
    rest="${BASH_REMATCH[4]}"
    if [[ -n "$rest" ]]; then
        patch=$((patch - 1))
    fi
    echo "${major}.${minor}.${patch}"
else
    echo "Unknown version format: $VERSION" >&2
    exit 1
fi
