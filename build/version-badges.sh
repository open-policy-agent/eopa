#!/bin/bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0

set -eou pipefail
REGAL=$(go list -m github.com/open-policy-agent/regal | cut -d' ' -f2)
OPA=$(go list -m github.com/open-policy-agent/opa | cut -d' ' -f2)

cat <<EOF
[![OPA ${OPA}](https://openpolicyagent.org/badge/${OPA})](https://github.com/open-policy-agent/opa/releases/tag/${OPA})
[![Regal ${REGAL}](https://img.shields.io/github/v/release/open-policy-agent/regal?filter=${REGAL}&label=Regal)](https://github.com/open-policy-agent/regal/releases/tag/${REGAL})
EOF
