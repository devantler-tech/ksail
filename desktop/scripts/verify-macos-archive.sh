#!/usr/bin/env bash
# Verify the signed app as it will be extracted from the release ZIP.
set -euo pipefail

if [[ $# != 1 || ! -f "$1" ]]; then
	echo "Expected one Apple Silicon desktop ZIP" >&2
	exit 1
fi

extracted="$(mktemp -d)"
trap 'rm -rf "$extracted"' EXIT
ditto -x -k "$1" "$extracted"
codesign --verify --deep --strict --verbose=2 "$extracted/KSail.app"
