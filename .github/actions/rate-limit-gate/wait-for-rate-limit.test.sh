#!/usr/bin/env bash
#
# Hermetic tests for wait-for-rate-limit.sh (#6291). A fake `gh` replays one scripted probe result
# per call (repeating the last one) and a fake `sleep` returns immediately, so no case touches the
# network. Only the hung-probe case waits, for about its short MAX_WAIT. An outage must never be
# reported as rate-limit exhaustion.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
gate="${RATE_LIMIT_GATE_SCRIPT:-${script_dir}/wait-for-rate-limit.sh}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

cat >"${fake_bin}/gh" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
count=$(($(cat "${FAKE_GH_COUNTER}") + 1))
printf '%s\n' "${count}" >"${FAKE_GH_COUNTER}"
total=$(wc -l <"${FAKE_GH_SEQUENCE}")
line_number=$((count < total ? count : total))
IFS='|' read -r code stdout stderr < <(sed -n "${line_number}p" "${FAKE_GH_SEQUENCE}")
if [ "${code}" = hang ]; then
	exec /bin/sleep 30
fi
[ -n "${stdout}" ] && printf '%s\n' "${stdout}"
[ -n "${stderr}" ] && printf '%s\n' "${stderr}" >&2
exit "${code}"
FAKE

printf '#!/usr/bin/env bash\nexit 0\n' >"${fake_bin}/sleep"
chmod +x "${fake_bin}/gh" "${fake_bin}/sleep"

pass_count=0

# run_case NAME EXPECTED_STATUS EXPECTED_TEXT FORBIDDEN_TEXT PROBE... — each PROBE is
# "exit-code|stdout|stderr" for one successive `gh api /rate_limit` call, or "hang||" for a call that
# never answers. CASE_MAX_WAIT overrides the gate's MAX_WAIT (default 60).
run_case() {
	local name="$1" expected_status="$2" expected_text="$3" forbidden_text="$4"
	shift 4

	local sequence="${tmp_dir}/${name}.sequence" counter="${tmp_dir}/${name}.counter"
	printf '%s\n' "$@" >"${sequence}"
	printf '0\n' >"${counter}"

	local output status
	set +e
	output="$(PATH="${fake_bin}:${PATH}" FAKE_GH_SEQUENCE="${sequence}" FAKE_GH_COUNTER="${counter}" \
		MIN_REMAINING=100 MAX_WAIT="${CASE_MAX_WAIT:-60}" bash "${gate}" 2>&1)"
	status=$?
	set -e

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' "${name}" "${expected_status}" "${status}" "${output}" >&2
		return 1
	fi
	if [[ "${output}" != *"${expected_text}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' "${name}" "${expected_text}" "${output}" >&2
		return 1
	fi
	if [[ -n "${forbidden_text}" && "${output}" == *"${forbidden_text}"* ]]; then
		printf 'FAIL: %s: output must not contain %q, got:\n%s\n' "${name}" "${forbidden_text}" "${output}" >&2
		return 1
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${name}"
}

run_case quota-available 0 'rate limit OK — 500 remaining' '' '0|500|'
run_case warning-on-stderr-does-not-corrupt-count 0 'rate limit OK — 500 remaining' '' \
	'0|500|A new release of gh is available'
run_case outage-recovers-within-budget 0 'rate limit OK — 500 remaining' 'rate limit exhausted' \
	'1||HTTP 503: No server is currently available to service your request' '0|500|'
run_case persistent-outage-is-not-exhaustion 1 'could not determine rate limit: API unreachable — the remaining quota is unknown, so this is not rate-limit exhaustion (gh exited 1: HTTP 503' 'rate limit exhausted' \
	'1||HTTP 503: No server is currently available to service your request'
run_case non-numeric-reply-is-not-exhaustion 1 "non-numeric remaining count 'null'" 'rate limit exhausted' \
	'0|null|'
run_case genuine-exhaustion 1 'rate limit exhausted (5 remaining, need 100)' 'unreachable' '0|5|'
run_case genuine-exhaustion-names-reset 1 'Resets at 2026-07-20T01:00:00Z' '' '0|5 2026-07-20T01:00:00Z|'
run_case exhaustion-without-reset-time-reports-unknown 1 'Resets at unknown' '' '0|5|'

# Once MAX_WAIT is spent, exhaustion must be reported from the last probe alone: any further gh call
# (here one that never answers) would push completion past the budget.
# The expected text leaves out the "Waited Ns" figure: the gate counts whole seconds, so a probe that
# straddles a second boundary honestly reports 1s. The duration check below bounds the time instead.
exhaustion_started=${SECONDS}
CASE_MAX_WAIT=0 run_case exhaustion-starts-no-lookup-after-budget 1 \
	'(max 0s). Resets at 2026-07-20T01:00:00Z.' \
	'unreachable' '0|5 2026-07-20T01:00:00Z|' 'hang||'
exhaustion_seconds=$((SECONDS - exhaustion_started))
if [[ "${exhaustion_seconds}" -gt 2 ]]; then
	printf 'FAIL: exhaustion-starts-no-lookup-after-budget: took %ss; a gh call ran after MAX_WAIT=0 was spent\n' \
		"${exhaustion_seconds}" >&2
	exit 1
fi

# A probe that never answers must be stopped within MAX_WAIT and reported as unreachable.
hang_started=${SECONDS}
CASE_MAX_WAIT=2 run_case hung-probe-is-bounded-and-not-exhaustion 1 \
	'this is not rate-limit exhaustion (gh did not answer within 2s)' 'rate limit exhausted' 'hang||'
hang_seconds=$((SECONDS - hang_started))
if [[ "${hang_seconds}" -gt 15 ]]; then
	printf 'FAIL: hung-probe-is-bounded-and-not-exhaustion: took %ss; the probe was not stopped near MAX_WAIT=2\n' \
		"${hang_seconds}" >&2
	exit 1
fi

# Missing authentication (gh exit status 4) cannot recover by waiting, so it must fail on the first
# probe with an authentication message, never as an outage or exhaustion.
run_case auth-failure-fails-fast 1 'gh is not authenticated (gh exited 4' 'unreachable' \
	'4||To get started with GitHub CLI, please run:  gh auth login'
auth_calls="$(cat "${tmp_dir}/auth-failure-fails-fast.counter")"
if [[ "${auth_calls}" -ne 1 ]]; then
	printf 'FAIL: auth-failure-fails-fast: expected exactly 1 gh call, got %s\n' "${auth_calls}" >&2
	exit 1
fi

# An invalid or expired token makes gh exit 1 with an HTTP 401, not 4. That rejection is just as
# permanent, so it must also fail on the first probe instead of retrying for the whole budget.
run_case rejected-credentials-fail-fast 1 \
	'GitHub rejected the credentials (gh exited 1: gh: Bad credentials (HTTP 401)' 'probe failed' \
	'1||gh: Bad credentials (HTTP 401)'
rejected_calls="$(cat "${tmp_dir}/rejected-credentials-fail-fast.counter")"
if [[ "${rejected_calls}" -ne 1 ]]; then
	printf 'FAIL: rejected-credentials-fail-fast: expected exactly 1 gh call, got %s\n' "${rejected_calls}" >&2
	exit 1
fi

# A 403 that refuses the token outright is a credential problem too.
run_case forbidden-token-fails-fast 1 \
	'GitHub rejected the credentials (gh exited 1: gh: Resource not accessible by integration (HTTP 403)' 'probe failed' \
	'1||gh: Resource not accessible by integration (HTTP 403)'
forbidden_calls="$(cat "${tmp_dir}/forbidden-token-fails-fast.counter")"
if [[ "${forbidden_calls}" -ne 1 ]]; then
	printf 'FAIL: forbidden-token-fails-fast: expected exactly 1 gh call, got %s\n' "${forbidden_calls}" >&2
	exit 1
fi

# GitHub also answers a secondary rate limit with a 403. That is throttling, not a bad token, so it must
# stay retryable and must never be reported as rejected credentials.
run_case secondary-rate-limit-403-is-not-auth 1 \
	'the rate-limit probe failed — the remaining quota is unknown, so this is not rate-limit exhaustion (gh exited 1: gh: You have exceeded a secondary rate limit' \
	'rejected the credentials' '1||gh: You have exceeded a secondary rate limit. Please wait a few minutes before you try again. (HTTP 403)'

# A generic gh failure that names no server or network error is a probe failure, not an outage: it
# must not tell the reader to wait for GitHub to recover.
run_case unclassified-probe-failure-is-not-outage 1 \
	'the rate-limit probe failed — the remaining quota is unknown, so this is not rate-limit exhaustion (gh exited 1: jq: error' \
	'API unreachable' '1||jq: error (at <stdin>:0): Cannot iterate over null'

# The caller's job timeout must leave room for the gate's own budget. Otherwise the runner cancels the
# job first and neither the outage nor the exhaustion message is ever reported.
ci_workflow="${script_dir}/../../workflows/ci.yaml"
gate_job="$(awk '/^  rate-limit-gate:$/ { inside = 1; print; next } inside && /^  [a-z]/ { exit } inside { print }' "${ci_workflow}")"
job_timeout="$(sed -n 's/^    timeout-minutes: *\([0-9][0-9]*\)$/\1/p' <<<"${gate_job}")"
gate_max_wait="$(sed -n 's/^ *max-wait: *"\{0,1\}\([0-9][0-9]*\)"\{0,1\}$/\1/p' <<<"${gate_job}")"
if [[ -z "${job_timeout}" || -z "${gate_max_wait}" ]]; then
	printf 'FAIL: ci-job-timeout-bounds-max-wait: rate-limit-gate job must set timeout-minutes and pass max-wait (got timeout=%q max-wait=%q)\n' \
		"${job_timeout}" "${gate_max_wait}" >&2
	exit 1
fi
if ((gate_max_wait + 60 > job_timeout * 60)); then
	printf 'FAIL: ci-job-timeout-bounds-max-wait: max-wait %ss leaves under 60s of the %s-minute job timeout\n' \
		"${gate_max_wait}" "${job_timeout}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: ci-job-timeout-bounds-max-wait\n'

printf 'All %s rate-limit gate cases passed.\n' "${pass_count}"
