#!/usr/bin/env bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0

# Script to get Regal version from go.mod file. The script removes the
# leading 'v' in the release tag. Example v0.8.0 -> 0.8.0.
# The script also trims whitespaces.
go list -m -json github.com/styrainc/regal | opa eval -fraw -I 'trim(input.Version, " v")'