#!/usr/bin/env bash
# wait-for-rate-limit.sh — wait until the GitHub API has at least MIN_REMAINING core calls left.
#
# The probe's exit status and output are checked before any comparison. An unreachable API or an
# unreadable reply leaves the remaining quota unknown, so it is retried within MAX_WAIT and, if it
# never recovers, reported as an outage. Only a readable count below MIN_REMAINING is reported as
# rate-limit exhaustion (#6291).
#
# Environment:
#   MIN_REMAINING  minimum remaining core API calls required to proceed
#   MAX_WAIT       maximum seconds to wait before failing

set -Eeuo pipefail

: "${MIN_REMAINING:?MIN_REMAINING is required}"
: "${MAX_WAIT:?MAX_WAIT is required}"

interval=30
max_interval=300
elapsed=0

stderr_file="$(mktemp)"
trap 'rm -f "${stderr_file}"' EXIT

while true; do
	probe_error=""
	if remaining="$(gh api /rate_limit --jq '.resources.core.remaining' 2>"${stderr_file}")"; then
		if [[ ! "${remaining}" =~ ^[0-9]+$ ]]; then
			probe_error="non-numeric remaining count '${remaining}'"
		elif [ "${remaining}" -ge "${MIN_REMAINING}" ]; then
			echo "✅ GitHub API rate limit OK — ${remaining} remaining (minimum: ${MIN_REMAINING})"
			exit 0
		fi
	else
		status=$?
		probe_error="gh exited ${status}: $(head -n 1 "${stderr_file}")"
	fi

	if [ "${elapsed}" -ge "${MAX_WAIT}" ]; then
		if [ -n "${probe_error}" ]; then
			echo "::error::could not determine rate limit: API unreachable — the remaining quota is unknown, so this is not rate-limit exhaustion (${probe_error}). Waited ${elapsed}s (max ${MAX_WAIT}s); re-run once GitHub recovers."
			exit 1
		fi

		reset_at="$(gh api /rate_limit --jq '.resources.core.reset | todate' 2>/dev/null)" || reset_at="unknown"
		echo "::error::GitHub API rate limit exhausted (${remaining} remaining, need ${MIN_REMAINING}). Waited ${elapsed}s (max ${MAX_WAIT}s). Resets at ${reset_at}."
		exit 1
	fi

	# Cap sleep to remaining budget so we don't overshoot MAX_WAIT
	budget=$((MAX_WAIT - elapsed))
	sleep_time=${interval}
	if [ "${sleep_time}" -gt "${budget}" ]; then
		sleep_time=${budget}
	fi

	if [ -n "${probe_error}" ]; then
		echo "⚠️ GitHub API unreachable (${probe_error}) — retrying in ${sleep_time}s (elapsed: ${elapsed}s / ${MAX_WAIT}s)"
	else
		echo "⏳ Rate limit low (${remaining} remaining, need ${MIN_REMAINING}) — retrying in ${sleep_time}s (elapsed: ${elapsed}s / ${MAX_WAIT}s)"
	fi
	sleep "${sleep_time}"
	elapsed=$((elapsed + sleep_time))
	interval=$((interval * 2))
	if [ "${interval}" -gt "${max_interval}" ]; then
		interval=${max_interval}
	fi
done
