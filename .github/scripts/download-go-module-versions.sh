#!/usr/bin/env bash
# Download exactly the named `module@version`s, with the same bounded retry as
# download-go-modules.sh (#8053). The provenance checks compare a local replacement
# with the upstream release it was reviewed against; that release is replaced in
# go.mod, so neither the full graph download nor a package download fetches it.
#
# Only this download is retried: the check reads the release from the module cache
# afterwards and runs once, so its own failure still stops the job. A missing version
# or a checksum mismatch fails here too, after the bounded attempts, with Go's own
# error left in the log. download-go-modules.sh stays separate because the release
# builds are audited against its exact content.
#
# Usage: download-go-module-versions.sh <module@version>...
set -euo pipefail

if [[ "$#" -eq 0 ]]; then
	printf '::error::download-go-module-versions.sh needs at least one module@version.\n' >&2
	exit 2
fi

for attempt in 1 2 3; do
	if go mod download "$@"; then
		exit 0
	fi

	if [[ "${attempt}" -eq 3 ]]; then
		printf '::error::Go module download failed after 3 attempts.\n' >&2
		exit 1
	fi

	delay=$((attempt * 5))
	printf '::warning::Go module download attempt %s/3 failed; retrying in %ss.\n' "${attempt}" "${delay}" >&2
	sleep "${delay}"
done
