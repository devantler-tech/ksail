#!/usr/bin/env bash
# Release tags must be semantic versions that can also be used as OCI tags.
set -euo pipefail
export LC_ALL=C

ref=${GITHUB_REF-}
tag=${ref#refs/tags/}
number='(0|[1-9][0-9]*)'
# Numeric prerelease identifiers cannot have leading zeros; identifiers containing
# a letter or hyphen may contain them. OCI tags do not permit '+' build metadata.
identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
pattern="^refs/tags/v${number}\\.${number}\\.${number}(-${identifier}(\\.${identifier})*)?$"

if [[ ${#tag} -gt 128 || ! $ref =~ $pattern ]]; then
	printf 'Invalid release ref %q: expected refs/tags/v<semver> with an OCI-compatible tag of at most 128 characters.\n' "$ref" >&2
	exit 1
fi

printf 'Validated release tag: %s\n' "$tag"
