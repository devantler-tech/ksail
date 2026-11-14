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
#
# mirror.gcr.io can refuse every attempt when its own quota runs out (#7922), so
# a cold worker then recovers from Amazon's anonymous copy of the same official
# image. That pull names a digest, so Docker verifies the content it receives. It
# is tagged with the name every helper container already uses, which keeps both
# actions on one image reference whichever source served it. The digest was the
# 1.37 index on both registries on 2026-11-01. If Amazon ever stops serving it,
# this recovery fails closed, before any mirror volume changes.
set -euo pipefail

helper_image=mirror.gcr.io/library/busybox:1.37
recovery_image=public.ecr.aws/docker/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec
attempts=3

# pull_with_retries <image>; returns non-zero once its attempts are spent.
pull_with_retries() {
	local attempt
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if docker pull --quiet "$1"; then
			return 0
		fi
		if ((attempt < attempts)); then
			echo "⚠️ Pull of mirror helper image $1 failed (attempt ${attempt}/${attempts}); retrying in $((attempt * 5))s…" >&2
			sleep "$((attempt * 5))"
		fi
	done
	return 1
}

if docker image inspect "$helper_image" >/dev/null 2>&1; then
	echo "✅ Mirror helper image ${helper_image} is already present"
	exit 0
fi

if pull_with_retries "$helper_image"; then
	echo "✅ Pulled mirror helper image ${helper_image}"
	exit 0
fi

echo "⚠️ Failed to pull the mirror helper image ${helper_image} after ${attempts} attempts; trying the digest-pinned copy" >&2
if pull_with_retries "$recovery_image"; then
	if docker tag "$recovery_image" "$helper_image"; then
		echo "✅ Recovered mirror helper image ${helper_image} from ${recovery_image}"
		exit 0
	fi
	echo "❌ Pulled ${recovery_image} but could not tag it as ${helper_image}; no mirror volume was changed" >&2
	exit 1
fi

echo "❌ Failed to pull the mirror helper image from ${helper_image} and ${recovery_image} after ${attempts} attempts each; no mirror volume was changed" >&2
exit 1
