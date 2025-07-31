#!/bin/bash
# Copyright 2025 The OPA Authors
# SPDX-License-Identifier: Apache-2.0

# This script automates the authenticode signing and timestamping process for a
# windows executable, using jsign.

dry_run=false
jar_path=""
positional_args=()

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
    --dry-run)
        if [[ $2 == "true" || $2 == "false" ]]; then
            dry_run=$2
            shift 2
        else
            echo "Error: --dry-run requires true or false"
            exit 1
        fi
        ;;
    --dry-run=*)
        dry_run="${1#*=}"
        shift
        ;;
    --jar-path)
        if [[ -n $2 && ! $2 =~ ^--.*$ ]]; then
            jar_path=$2
            shift 2
        else
            echo "Error: --jar-path requires a valid path"
            exit 1
        fi
        ;;
    --jar-path=*)
        jar_path="${1#*=}"
        shift
        ;;
    *)
        positional_args+=("$1")
        shift
        ;;
    esac
done

# Define the jsign function

# jsign() {
#     local jar="${jar_path}"

#     # Check if the JAR file exists
#     if [[ ! -f "$jar" ]]; then
#         echo "Error: jsign jar file not found at: $jar" >&2
#         return 1
#     fi

#     java -jar "${jar_path}" "${positional_args[@]}"
# }

# Validate dry_run value
if [[ "$dry_run" != "true" && "$dry_run" != "false" ]]; then
    echo "Error: --dry-run must be either 'true' or 'false'"
    exit 1
fi

# Early-exit if dry_run is set.
if [[ "$dry_run" == "true" ]]; then
    echo "Info: Exiting early due to --dry-run. Executable will not be signed."
    exit 0
fi

# Ensure at least one positional file argument is set.
if [ ${#positional_args} -eq 0 ] || [ ! -f "${positional_args[0]}" ]; then
    echo "Error: Please provide at least one valid file argument."
    exit 1
fi

# Parameter expansion + ternary trick for POSIX shell env var set guards:
# https://stackoverflow.com/a/307735
: "${CERT_ID:?Environment variable CERT_ID is not set or empty}"
: "${SM_API_KEY:?Environment variable SM_API_KEY is not set or empty}"
: "${SM_CLIENT_CERT_FILE:?Environment variable SM_CLIENT_CERT_FILE is not set or empty}"
: "${SM_CLIENT_CERT_PASSWORD:?Environment variable SM_CLIENT_CERT_PASSWORD is not set or empty}"

# Define jsign function based on whether jar_path was provided.
if [[ -n "$jar_path" ]]; then
    jsign() {
        # Check if the JAR file exists
        if [[ ! -f "$jar_path" ]]; then
            echo "Error: jsign jar file not found at: $jar_path" >&2
            return 1
        fi

        # Run the command with the provided JAR
        if [[ "$dry_run" != "true" ]]; then
            java -jar "$jar_path" "$@"
        fi
    }
elif ! command -v jsign &> /dev/null; then
    # If jar_path wasn't provided and jsign isn't on the path,
    # print an error message
    echo "Error: jsign command not found and no --jar-path provided" >&2
    exit 1
fi

jsign --storetype DIGICERTONE \
      --storepass "$SM_API_KEY|$SM_CLIENT_CERT_FILE|$SM_CLIENT_CERT_PASSWORD" \
      --keystore "https://clientauth.one.nl.digicert.com" \
      --tsaurl="http://timestamp.digicert.com" \
      --alias $CERT_ID "${positional_args[@]}"

