#!/usr/bin/env bash
# wait-for-rate-limit.sh — wait until the GitHub API has at least MIN_REMAINING core calls left.
#
# The probe's exit status and output are checked before any comparison. A probe that does not answer
# in time, or that reports a server or network error, means the API is unreachable. Any other failure,
# including an unreadable reply, is a probe failure. Both leave the remaining quota unknown, so they
# are retried and, if they never recover, reported as what they are. Missing authentication (gh exit
# status 4) and credentials GitHub rejects (HTTP 401, or a 403 that is not a rate limit) fail
# immediately, because retrying cannot fix them. Only a readable count below MIN_REMAINING is
# reported as rate-limit exhaustion (#6291).
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
	probe_kind=""
	remaining=""
	reset_at="unknown"
	budget=$((MAX_WAIT - elapsed))
	limit=$((budget < 1 ? 1 : (budget > 60 ? 60 : budget)))
	started=${SECONDS}
	# One probe reads the reset time with the count, so reporting exhaustion never needs another call
	# after the budget is spent.
	if run_bounded "${limit}" gh api /rate_limit --jq '.resources.core | "\(.remaining) \(.reset | todate)"'; then
		read -r remaining reset <"${stdout_file}" || true
		if [[ "${reset:-}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
			reset_at="${reset}"
		fi
		if [[ ! "${remaining}" =~ ^[0-9]+$ ]]; then
			probe_error="non-numeric remaining count '${remaining}'"
			probe_kind="probe failed"
		elif [ "${remaining}" -ge "${MIN_REMAINING}" ]; then
			echo "✅ GitHub API rate limit OK — ${remaining} remaining (minimum: ${MIN_REMAINING})"
			exit 0
		fi
	else
		status=$?
		if [ "${status}" -eq 124 ]; then
			probe_error="gh did not answer within ${limit}s"
			probe_kind="unreachable"
		elif [ "${status}" -eq 4 ]; then
			# gh documents exit status 4 as "authentication required": no amount of waiting fixes that.
			echo "::error::could not determine rate limit: gh is not authenticated (gh exited 4: $(head -n 1 "${stderr_file}")). Check that GH_TOKEN is set and valid; retrying will not help."
			exit 1
		elif grep -Eqi 'HTTP 401|Bad credentials' "${stderr_file}" ||
			{ grep -Eqi 'HTTP 403|Forbidden' "${stderr_file}" && ! grep -Eqi 'rate limit' "${stderr_file}"; }; then
			# An invalid or expired token makes gh exit 1, not 4, so detect the rejection itself. A 403 that
			# mentions a rate limit is GitHub throttling, not a credential problem, and stays retryable.
			echo "::error::could not determine rate limit: GitHub rejected the credentials (gh exited ${status}: $(head -n 1 "${stderr_file}")). Check that GH_TOKEN is valid and not expired; retrying will not help."
			exit 1
		else
			probe_error="gh exited ${status}: $(head -n 1 "${stderr_file}")"
			# Status 1 means failure "for any reason", so only a server or network error counts as unreachable.
			if grep -Eqi 'HTTP 5[0-9]{2}|connection (refused|reset)|i/o timeout|timed out|no such host|could not resolve|network is unreachable|TLS handshake|unexpected EOF' "${stderr_file}"; then
				probe_kind="unreachable"
			else
				probe_kind="probe failed"
			fi
		fi
	fi
	elapsed=$((elapsed + SECONDS - started))

	if [ "${elapsed}" -ge "${MAX_WAIT}" ]; then
		if [ "${probe_kind}" = "unreachable" ]; then
			echo "::error::could not determine rate limit: API unreachable — the remaining quota is unknown, so this is not rate-limit exhaustion (${probe_error}). Waited ${elapsed}s (max ${MAX_WAIT}s); re-run once GitHub recovers."
			exit 1
		fi
		if [ -n "${probe_error}" ]; then
			echo "::error::could not determine rate limit: the rate-limit probe failed — the remaining quota is unknown, so this is not rate-limit exhaustion (${probe_error}). Waited ${elapsed}s (max ${MAX_WAIT}s); check the gh error, since re-running may not help."
			exit 1
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

	if [ "${probe_kind}" = "unreachable" ]; then
		echo "⚠️ GitHub API unreachable (${probe_error}) — retrying in ${sleep_time}s (elapsed: ${elapsed}s / ${MAX_WAIT}s)"
	elif [ -n "${probe_error}" ]; then
		echo "⚠️ Rate-limit probe failed (${probe_error}) — retrying in ${sleep_time}s (elapsed: ${elapsed}s / ${MAX_WAIT}s)"
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
