#!/usr/bin/env bash

set -e

SRC_DIR=$(dirname "${BASH_SOURCE}")/..
CHANGELOG="${SRC_DIR}/CHANGELOG.md"

usage() {
    echo "latest-release-notes.sh --output=<path>"
}

OUTPUT=""

for i in "$@"; do
    case $i in
    --output=*)
        OUTPUT="${i#*=}"
        shift
        ;;
    *)
        usage
        exit 1
        ;;
    esac
done

if [ -z "${OUTPUT}" ]; then
    usage
    exit 1
fi

# Versions start with a h2 (## v<semver>), find the latest two for start and stop
# positions in the CHANGELOG
LATEST_VERSION=$(grep '## v[0-9]' "${CHANGELOG}" | head -n 1)
STOP_VERSION=$(grep '## v[0-9]' "${CHANGELOG}" | head -n 2 | tail -n 1)

STARTED=false

while IFS= read -r line
do
    # Skip lines until the first version header is found
    if [[ "${STARTED}" == false ]]; then
        if [[ "${line}" == "${LATEST_VERSION}" ]]; then
            STARTED=true
            echo "START"
        fi
        continue
    fi

    # Stop reading after we see the stopping point
    if [[ "${line}" == "${STOP_VERSION}" ]]; then
        echo "FOUND STOP"
        break
    fi

    # Append each line between the two onto the release notes
    echo -e "${line}" >> "${OUTPUT}"

done < "${CHANGELOG}"

# Delete all leading blank lines at top of file
sed -i.bak '/./,$!d' "${OUTPUT}"
rm "${OUTPUT}.bak"
