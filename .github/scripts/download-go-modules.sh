#!/usr/bin/env bash
# Warm the module cache before compilation or a provenance check. Only downloads are
# retried: a compiler, GoReleaser or verifier failure must still stop immediately.
#
# With no arguments it downloads the whole module graph. With `module@version`
# arguments it downloads only those modules, which is how the provenance checks
# fetch the exact upstream release a local replacement is compared against (#8053).
# A missing version or a checksum mismatch still fails after the bounded attempts,
# with Go's own error left in the log.
#
# Usage: download-go-modules.sh [module@version]...
set -euo pipefail

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
