#!/usr/bin/env bash
# wait-for-rate-limit.sh — wait until the GitHub API has at least MIN_REMAINING core calls left.
#
# The probe's exit status and output are checked before any comparison. An unreachable API, an
# unreadable reply, or a probe that does not answer in time leaves the remaining quota unknown, so it
# is retried and, if it never recovers, reported as an outage. Only a readable count below
# MIN_REMAINING is reported as rate-limit exhaustion (#6291).
#
# MAX_WAIT bounds the whole wait: time spent in probes counts toward it alongside the sleeps between
# them, and each probe is stopped once the remaining budget is spent.
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

stdout_file="$(mktemp)"
stderr_file="$(mktemp)"
timeout_marker="$(mktemp)"
trap 'rm -f "${stdout_file}" "${stderr_file}" "${timeout_marker}"' EXIT

# run_bounded LIMIT COMMAND... runs COMMAND with its output in stdout_file and stderr_file and stops
# it after LIMIT seconds. It returns COMMAND's status, or 124 when LIMIT was reached. The watchdog
# calls /bin/sleep directly so the deadline stays real even where `sleep` is replaced.
run_bounded() {
	local limit="$1" pid watchdog status
	shift
	rm -f "${timeout_marker}"
	"$@" </dev/null >"${stdout_file}" 2>"${stderr_file}" &
	pid=$!
	(
		trap 'kill "${sleeper:-}" 2>/dev/null; exit 0' TERM
		/bin/sleep "${limit}" &
		sleeper=$!
		wait "${sleeper}" && : >"${timeout_marker}" && kill "${pid}" 2>/dev/null
	) </dev/null >/dev/null 2>&1 &
	watchdog=$!
	if wait "${pid}"; then
		status=0
	else
		status=$?
	fi
	kill "${watchdog}" 2>/dev/null || true
	wait "${watchdog}" 2>/dev/null || true
	if [ -e "${timeout_marker}" ]; then
		return 124
	fi
	return "${status}"
}

while true; do
	probe_error=""
	remaining=""
	budget=$((MAX_WAIT - elapsed))
	limit=$((budget < 1 ? 1 : (budget > 60 ? 60 : budget)))
	started=${SECONDS}
	if run_bounded "${limit}" gh api /rate_limit --jq '.resources.core.remaining'; then
		remaining="$(cat "${stdout_file}")"
		if [[ ! "${remaining}" =~ ^[0-9]+$ ]]; then
			probe_error="non-numeric remaining count '${remaining}'"
		elif [ "${remaining}" -ge "${MIN_REMAINING}" ]; then
			echo "✅ GitHub API rate limit OK — ${remaining} remaining (minimum: ${MIN_REMAINING})"
			exit 0
		fi
	else
		status=$?
		if [ "${status}" -eq 124 ]; then
			probe_error="gh did not answer within ${limit}s"
		else
			probe_error="gh exited ${status}: $(head -n 1 "${stderr_file}")"
		fi
	fi
	elapsed=$((elapsed + SECONDS - started))

	if [ "${elapsed}" -ge "${MAX_WAIT}" ]; then
		if [ -n "${probe_error}" ]; then
			echo "::error::could not determine rate limit: API unreachable — the remaining quota is unknown, so this is not rate-limit exhaustion (${probe_error}). Waited ${elapsed}s (max ${MAX_WAIT}s); re-run once GitHub recovers."
			exit 1
		fi

		if run_bounded 10 gh api /rate_limit --jq '.resources.core.reset | todate'; then
			reset_at="$(cat "${stdout_file}")"
		else
			reset_at="unknown"
		fi
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
