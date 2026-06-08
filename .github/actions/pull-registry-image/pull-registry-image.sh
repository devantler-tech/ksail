#!/usr/bin/env bash
# Reliably obtain the registry:3 image that backs KSail's local pull-through
# mirror registries.
#
# Single source of truth shared by two composite actions:
#   - .github/actions/pull-registry-image   → bash "$GITHUB_ACTION_PATH/pull-registry-image.sh"
#   - .github/actions/ksail-cluster         → bash "$GITHUB_ACTION_PATH/../pull-registry-image/pull-registry-image.sh"
# ksail-cluster cannot `uses:` the sibling action directly: a `./` path in a
# composite action resolves against the consumer's workspace, not this repo, so
# it breaks every external caller (actions/runner#1348). Running this script via
# $GITHUB_ACTION_PATH avoids that without duplicating the logic or pinning a ref.
#
# set flags must live HERE, not in the caller's step: `bash <file>` starts a
# fresh shell, so the calling step's `set -euo pipefail` does not propagate in.
set -euo pipefail

# mirror.gcr.io is Google's anonymous pull-through cache for Docker Hub;
# it is not subject to Docker Hub's per-IP anonymous rate limits, which is
# what causes the recurring "registry:3 pull timeout" flake on shared CI
# runners. Try the mirror first, then fall back to Docker Hub directly,
# and re-tag whichever source succeeds to the canonical registry:3.
obtain_registry_image() {
	local ref attempt
	for ref in mirror.gcr.io/library/registry:3 registry:3; do
		for attempt in 1 2 3; do
			if docker pull --quiet "$ref"; then
				docker tag "$ref" registry:3
				echo "✅ registry:3 obtained from ${ref}"
				return 0
			fi
			echo "⚠️ pull ${ref} attempt ${attempt}/3 failed; retrying in $((attempt * 5))s…"
			sleep "$((attempt * 5))"
		done
	done
	return 1
}

if ! obtain_registry_image; then
	echo "❌ Failed to obtain registry:3 from mirror.gcr.io and Docker Hub"
	exit 1
fi
