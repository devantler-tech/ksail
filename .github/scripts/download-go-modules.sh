#!/usr/bin/env bash
# Warm the module cache before release compilation. Only downloads are retried:
# a compiler or GoReleaser failure must still stop immediately.
set -euo pipefail

for attempt in 1 2 3; do
	if go mod download; then
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
