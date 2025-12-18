#!/bin/bash

# Script to generate JSON schema from KSail config types
# This script is used by the pre-commit hook

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GENERATE_SCHEMA_DIR="$SCRIPT_DIR/generate-schema"

echo "Generating JSON schema from KSail config types..."

# Change to the generate-schema module directory before running
cd "$GENERATE_SCHEMA_DIR"

# Run the schema generator from the generate-schema module
if ! go run main.go "$REPO_ROOT/schemas/ksail-config.schema.json"; then
	echo "Error: Failed to generate JSON schema. Check the output above for details." >&2
	exit 1
fi

echo "JSON schema generation completed successfully"
