#!/usr/bin/env bash

set -e

usage() {
    echo "gen-release-patch.sh --version=<mj.mn.pt>"
}

for i in "$@"; do
    case $i in
    --version=*)
        VERSION="${i#*=}"
        shift
        ;;
    *)
        usage
        exit 1
        ;;
    esac
done

if [ -z "$VERSION" ]; then
    usage
    exit 1
fi

update_capabilities() {
    mkdir -p capabilities
    cp capabilities.json capabilities/v$VERSION.json
    # Use --intent-to-add so that new file shows up in git diff
    git add --intent-to-add capabilities/v$VERSION.json
}

go generate
update_capabilities
