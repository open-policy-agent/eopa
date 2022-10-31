#!/usr/bin/env bash
# Script to get OPA version from go.mod file. The script removes the
# leading 'v' in the OPA release tag. Example v0.8.0 -> 0.8.0.
# The script also trims whitespaces.
go list -m -json github.com/open-policy-agent/opa | opa eval -fraw -I 'trim(input.Version, " v")'
