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
if [[ "$*" == *reset* ]]; then
	printf '2026-07-20T01:00:00Z\n'
	exit 0
fi
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
run_case genuine-exhaustion-names-reset 1 'Resets at 2026-07-20T01:00:00Z' '' '0|5|'

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

printf 'All %s rate-limit gate cases passed.\n' "${pass_count}"
