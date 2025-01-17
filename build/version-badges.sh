#!/bin/bash
set -eou pipefail
REGAL=$(go list -m github.com/styrainc/regal | cut -d' ' -f2)
OPA=$(go list -m github.com/open-policy-agent/opa | cut -d' ' -f2)

cat <<EOF
[![OPA ${OPA}](https://img.shields.io/endpoint?url=https://openpolicyagent.org/badge-endpoint/${OPA})](https://github.com/open-policy-agent/opa/releases/tag/${OPA})
[![Regal ${REGAL}](https://img.shields.io/github/v/release/styrainc/regal?filter=${REGAL}&label=Regal)](https://github.com/StyraInc/regal/releases/tag/${REGAL})
EOF
