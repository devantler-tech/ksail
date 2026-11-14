#!/usr/bin/env bash

set -euo pipefail

# Keep the complete compiler recipe in one cache-keyed file. This source is the
# immutable legacy checkout, never the candidate or a parent Go workspace.
export GOWORK=off
mkdir -p "${RUNNER_TEMP:?}/calico-legacy-bin"
(
	cd calico-legacy-source
	go build -trimpath -ldflags='-s -w' -o "$RUNNER_TEMP/calico-legacy-bin/ksail" .
)
