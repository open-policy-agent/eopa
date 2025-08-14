#!/bin/bash
# Run goversioninfo to generate the resource.syso to embed version info.
set -eux

NAME="EOPA"
VERSION=$(git describe --abbrev=0 --tags | sed s/^v//)
FLAGS=()

# If building for arm64, then include the extra flags required.
if [ -n "${1+x}" ] && [ "$1" = "arm64" ]; then
    FLAGS=(-arm -64)
fi

if ! command -v goversioninfo &> /dev/null; then
    # If goversioninfo isn't on the path, print an error message
    echo "Error: goversioninfo command not found" >&2
    exit 1
fi

goversioninfo "${FLAGS[@]}" \
    -product-name "$NAME" \
    -product-version "$VERSION" \
    -copyright "Open Policy Agent" \
    -skip-versioninfo \
    -o resource.syso
