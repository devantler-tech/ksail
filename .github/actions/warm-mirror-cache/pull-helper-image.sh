#!/usr/bin/env bash
# Obtain the small helper image that packs and unpacks mirror volumes.
#
# Shared by the warm and restore actions, which both call it before they start a
# helper container or change a mirror volume:
#   - .github/actions/warm-mirror-cache    → bash "$GITHUB_ACTION_PATH/pull-helper-image.sh"
#   - .github/actions/restore-mirror-cache → bash "$GITHUB_ACTION_PATH/../warm-mirror-cache/pull-helper-image.sh"
#
# `docker run` pulls a missing image exactly once, so one short-lived registry
# error on this image used to be reported as a failed mirror import (#7673).
# Pulling it here, with a bounded retry, keeps that error apart from the mirrors.
#
# The image comes from mirror.gcr.io, Google's anonymous pull-through cache for
# Docker Hub. There is deliberately no Docker Hub fallback: the runner is logged
# in to Docker Hub, so a direct pull counts against the authenticated rate limit
# and can leak the session token into error logs when throttled.
set -euo pipefail

helper_image=mirror.gcr.io/library/busybox:1.37
attempts=3

if docker image inspect "$helper_image" >/dev/null 2>&1; then
	echo "✅ Mirror helper image ${helper_image} is already present"
	exit 0
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
	if docker pull --quiet "$helper_image"; then
		echo "✅ Pulled mirror helper image ${helper_image}"
		exit 0
	fi
	if ((attempt < attempts)); then
		echo "⚠️ Pull of mirror helper image ${helper_image} failed (attempt ${attempt}/${attempts}); retrying in $((attempt * 5))s…" >&2
		sleep "$((attempt * 5))"
	fi
done

echo "❌ Failed to pull the mirror helper image ${helper_image} after ${attempts} attempts; no mirror volume was changed" >&2
exit 1
