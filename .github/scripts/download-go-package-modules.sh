#!/usr/bin/env bash
# Download only the modules the named packages need, with the same bounded retry as
# download-go-modules.sh (#8053). The narrow Windows and macOS checks build a handful of
# files, and those runners have no shared module archive, so a full `go mod download`
# would turn their small per-package fetch into the whole graph on every run.
#
# `go list -deps -test` loads every package the later build or test step compiles, which
# fetches exactly those modules and nothing else. Only this download is retried: the
# caller's own build or test runs afterwards, once, so its failure still stops the job.
# A missing version or a checksum mismatch fails here too, after the bounded attempts,
# with Go's own error left in the log.
#
# Usage: download-go-package-modules.sh <package or .go file>...
set -euo pipefail

if [[ "$#" -eq 0 ]]; then
	printf '::error::download-go-package-modules.sh needs at least one package or Go file.\n' >&2
	exit 2
fi

for attempt in 1 2 3; do
	if go list -deps -test "$@" >/dev/null; then
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
