#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
wait_script="${script_dir}/wait-for-github-api-budget.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

test_timeout=timeout
if ! command -v "${test_timeout}" >/dev/null 2>&1; then
	test_timeout=gtimeout
fi
command -v "${test_timeout}" >/dev/null

# The fake clock starts at 1000 and moves only when the script sleeps, so every wait is exact and
# the test never really waits.
cat >"${fake_bin}/date" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "+%s" ]] || { printf 'unexpected fake date invocation: %s\n' "$*" >&2; exit 2; }
cat "${BUDGET_STATE_DIR:?}/now"
EOF

cat >"${fake_bin}/sleep" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
state_dir="${BUDGET_STATE_DIR:?}"
printf '%s\n' "$1" >>"${state_dir}/sleeps"
printf '%s\n' "$(($(cat "${state_dir}/now") + $1))" >"${state_dir}/now"
EOF

# Each scenario lists one response per read; the last one repeats. A response is either
# "<core remaining> <core reset> <graphql remaining> <graphql reset>", "fail" or "garbage".
cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
state_dir="${BUDGET_STATE_DIR:?}"
reads="$(cat "${state_dir}/reads")"
total="$(wc -l <"${state_dir}/responses" | tr -d ' ')"
case "$*" in
"api rate_limit" | "api --include rate_limit")
	line=$((reads + 1))
	printf '%s\n' "${line}" >"${state_dir}/reads"
	;;
"api --include graphql -f query=query { rateLimit { remaining resetAt } }") line="${reads}" ;;
*) printf 'unexpected fake gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
[[ "${line}" -gt "${total}" ]] && line="${total}"
response="$(sed -n "${line}p" "${state_dir}/responses")"
behavior="${response}"
case "${behavior}" in
core-failed-full-output | graphql-failed-full-output | graphql-partial | graphql-partial-core-exhausted | graphql-wrong-resource | graphql-duplicate-header | graphql-missing-header | graphql-other-error | graphql-rate-limited-positive-header | core-hangs | graphql-hangs | core-headers-exhausted | core-headers-recovered | core-wrong-resource | core-duplicate-header | core-missing-header | core-forbidden | core-secondary-* | graphql-secondary-*)
	response="4000 5000 4900 5000"
	;;
graphql-rate-limit-alias) response="4000 1100 0 1300" ;;
esac
[[ "${behavior}" == core-headers-recovered ]] && response="0 5000 4900 5000"
[[ "${behavior}" == graphql-secondary-after-core-exhaustion ]] && response="0 1600 4900 5000"
[[ "${behavior}" == graphql-partial-core-exhausted ]] && response="0 1600 4900 5000"
if [[ "${behavior}" == core-hangs && ( "$*" == "api rate_limit" || "$*" == "api --include rate_limit" ) ]] ||
	[[ "${behavior}" == graphql-hangs && "$*" == "api --include graphql "* ]]; then
	exec /bin/sleep 60
fi
# Secondary refusals must be distinguished from ordinary permission failures.
if [[ "${behavior}" == core-secondary-* && "$*" != "api --include graphql "* ]] ||
	[[ "${behavior}" == graphql-secondary-* && "$*" == "api --include graphql "* ]] ||
	[[ "${behavior}" == core-forbidden && "$*" != "api --include graphql "* ]]; then
	if [[ "$*" == "api --include "* ]]; then
		resource=core
		[[ "${behavior}" == graphql-secondary-* ]] && resource=graphql
		remaining=4000
		[[ "${behavior}" == core-secondary-zero-header ]] && remaining=0
		printf 'HTTP/2.0 403 Forbidden\r\nX-Ratelimit-Resource: %s\r\nX-Ratelimit-Remaining: %s\r\nX-Ratelimit-Reset: 1300\r\n' "${resource}" "${remaining}"
		case "${behavior}" in
		*-secondary-retry | core-secondary-zero-header | graphql-secondary-after-core-exhaustion) printf 'Retry-After: 120\r\n' ;;
		core-secondary-short-retry) printf 'Retry-After: 5\r\n' ;;
		core-secondary-bad-retry) printf 'Retry-After: soon\r\n' ;;
		esac
		printf 'X-Private-Value: private-sentinel\r\n\r\n'
	fi
	if [[ "${behavior}" == core-forbidden ]]; then
		printf '%s\n' '{"message":"Forbidden"}'
	else
		printf '%s\n' '{"message":"You have exceeded a secondary rate limit."}'
	fi
	exit 1
fi
if [[ "$*" == "api --include graphql "* ]]; then
	read -r _ _ reported_remaining reported_reset actual_remaining actual_reset <<<"${response}"
	actual_remaining="${actual_remaining:-${reported_remaining}}"
	actual_reset="${actual_reset:-${reported_reset}}"
	resource=graphql
	[[ "${behavior}" == graphql-wrong-resource ]] && resource=core
	printf 'HTTP/2.0 200 OK\r\nX-Ratelimit-Resource: %s\r\nX-Ratelimit-Remaining: %s\r\n' \
		"${resource}" "${actual_remaining}"
	[[ "${behavior}" == graphql-duplicate-header ]] && printf 'X-Ratelimit-Remaining: 4900\r\n'
	[[ "${behavior}" != graphql-missing-header ]] && printf 'X-Ratelimit-Reset: %s\r\n' "${actual_reset}"
	printf 'X-Private-Value: private-sentinel\r\n\r\n'
	case "${behavior}" in
	graphql-partial | graphql-partial-core-exhausted) printf '%s' '{"data":{"rateLimit":'; exit 1 ;;
	graphql-other-error) printf '%s\n' '{"errors":[{"type":"FORBIDDEN"}]}'; exit 1 ;;
	graphql-rate-limited-positive-header) printf '%s\n' '{"errors":[{"type":"RATE_LIMITED"}]}'; exit 1 ;;
	esac
	if [[ "${actual_remaining}" == 0 ]]; then
		rate_error=RATE_LIMITED
		[[ "${behavior}" == graphql-rate-limit-alias ]] && rate_error=RATE_LIMIT
		printf '{"errors":[{"type":"%s"}]}\n' "${rate_error}"
		exit 1
	fi
	printf '{"data":{"rateLimit":{"remaining":%s,"resetAt":"2026-10-13T15:00:00Z"}}}\n' "${actual_remaining}"
	[[ "${behavior}" == graphql-failed-full-output ]] && exit 1
	exit 0
fi
if [[ "$*" == "api --include rate_limit" ]]; then
	read -r core_remaining core_reset _ <<<"${response}"
	resource=core
	[[ "${behavior}" == core-wrong-resource ]] && resource=graphql
	[[ "${behavior}" == core-headers-exhausted ]] && core_remaining=0 && core_reset=1300
	[[ "${behavior}" == core-headers-recovered ]] && core_remaining=4000
	printf 'HTTP/2.0 200 OK\r\nX-Ratelimit-Resource: %s\r\nX-Ratelimit-Remaining: %s\r\n' "${resource}" "${core_remaining}"
	[[ "${behavior}" == core-duplicate-header ]] && printf 'X-Ratelimit-Remaining: 4000\r\n'
	[[ "${behavior}" != core-missing-header ]] && printf 'X-Ratelimit-Reset: %s\r\n' "${core_reset}"
	printf 'X-Private-Value: private-sentinel\r\n\r\n'
fi
case "${response}" in
fail) exit 1 ;;
garbage) printf '%s\n' '{"resources":{"core":{"remaining":"many","reset":null}}}' ;;
*)
	read -r core_remaining core_reset graphql_remaining graphql_reset _ <<<"${response}"
	printf '{"resources":{"core":{"remaining":%s,"reset":%s},"graphql":{"remaining":%s,"reset":%s}}}\n' \
		"${core_remaining}" "${core_reset}" "${graphql_remaining}" "${graphql_reset}"
	[[ "${behavior}" == core-failed-full-output ]] && exit 1
	;;
esac
exit 0
EOF
chmod +x "${fake_bin}/date" "${fake_bin}/sleep" "${fake_bin}/gh"

failures=0
fail() {
	printf 'FAIL: %s\n' "$1" >&2
	failures=$((failures + 1))
}

# run_case <name> <expected exit> <expected sleeps, space separated> <expected output text> <responses...> [-- script args]
run_case() {
	local name="$1" want_status="$2" want_sleeps="$3" want_text="$4"
	shift 4
	local state_dir="${tmp_dir}/${name}" output status=0 sleeps
	mkdir -p "${state_dir}"
	printf '1000\n' >"${state_dir}/now"
	printf '0\n' >"${state_dir}/reads"
	: >"${state_dir}/sleeps"
	: >"${state_dir}/responses"
	while [[ $# -gt 0 && "$1" != -- ]]; do
		printf '%s\n' "$1" >>"${state_dir}/responses"
		shift
	done
	[[ $# -gt 0 ]] && shift
	output="$(PATH="${fake_bin}:${PATH}" BUDGET_STATE_DIR="${state_dir}" \
		"${test_timeout}" -k 2s 15s bash "${wait_script}" "$@" 2>&1)" || status=$?
	sleeps="$(tr '\n' ' ' <"${state_dir}/sleeps" | sed 's/ $//')"
	if [[ "${status}" -ne "${want_status}" ]]; then
		fail "${name}: exit ${status}, expected ${want_status}. Output: ${output}"
	fi
	if [[ "${sleeps}" != "${want_sleeps}" ]]; then
		fail "${name}: slept '${sleeps}', expected '${want_sleeps}'"
	fi
	if [[ "${output}" != *"${want_text}"* ]]; then
		fail "${name}: output lacks '${want_text}'. Output: ${output}"
	fi
	if [[ "${output}" == *private-sentinel* ]]; then
		fail "${name}: raw response headers escaped into output"
	fi
}

# Budget present: no wait at all.
run_case available 0 "" "budget is available: REST 4000, GraphQL 4900" \
	"4000 5000 4900 5000"

# The failure this exists for: REST at zero, reset 600 s away. One wait of 605 s, then proceed.
run_case rest-exhausted 0 "605" "budget is available: REST 5000" \
	"0 1600 4900 1600" "5000 5200 4900 5200"

# Below the floor counts as exhausted, and exactly at the floor does not.
run_case below-floor 0 "105" "waiting 105 s" \
	"299 1100 4900 1100" "5000 4700 4900 4700"
run_case at-floor 0 "" "budget is available: REST 300, GraphQL 50" \
	"300 1100 50 1100"

# GraphQL alone exhausted, and both exhausted with GraphQL resetting later: wait for the later one.
run_case graphql-exhausted 0 "305" "budget is available" \
	"4000 1100 0 1300" "4000 1100 5000 4900"
run_case graphql-rate-limit-alias 0 "305" "budget is available" \
	"graphql-rate-limit-alias" "4000 1100 5000 4900"
run_case graphql-rate-limited-deadline 1 "" "does not return within 100 s" \
	"4000 1100 0 1300" -- --max-wait 100
run_case graphql-rate-limited-positive-header 0 "10 10" "Could not read" \
	"graphql-rate-limited-positive-header"
run_case both-exhausted 0 "905" "budget is available" \
	"0 1100 0 1900" "5000 4700 5000 5500"

# The GraphQL endpoint's own response determines its allowance. The REST
# endpoint can report a different GraphQL window on the same authenticated CLI.
run_case graphql-endpoint-exhausted 0 "905" "budget is available" \
	"4000 1600 4900 1100 0 1900" "4000 5200 4900 1100 5000 5200"
run_case graphql-rest-metadata-stale 0 "" "budget is available" \
	"4000 1600 0 1900 4900 1600" "4000 5200 4900 5200 4900 5200"

# REST's own headers, rather than its cross-region JSON snapshot, control the wait.
run_case core-headers-exhausted 0 "305" "budget is available" \
	"core-headers-exhausted" "4000 5000 4900 5000"
run_case core-headers-recovered 0 "" "REST 4000, GraphQL 4900" "core-headers-recovered"
run_case core-exhausted-with-unreadable-graphql 0 "605" "budget is available" \
	"graphql-partial-core-exhausted" "4000 5000 4900 5000"
run_case core-exhausted-with-unreadable-graphql-at-deadline 1 "" "does not return within 0 s" \
	"graphql-partial-core-exhausted" -- --max-wait 0
for invalid in core-wrong-resource core-duplicate-header core-missing-header core-forbidden; do
	run_case "${invalid}" 0 "10 10" "Could not read" "${invalid}"
done

# Known secondary restrictions never fall through to API-heavy delivery.
run_case core-secondary-retry 0 "120" "budget is available" \
	"core-secondary-retry" "4000 5000 4900 5000"
run_case graphql-secondary-retry 0 "120" "budget is available" \
	"graphql-secondary-retry" "4000 5000 4900 5000"
run_case secondary-without-retry-after 0 "60" "budget is available" \
	"core-secondary-default" "4000 5000 4900 5000"
run_case secondary-exponential-backoff 0 "60 120" "budget is available" \
	"core-secondary-default" "core-secondary-default" "4000 5000 4900 5000"
run_case secondary-retry-after-exponential 0 "120 240" "budget is available" \
	"core-secondary-retry" "core-secondary-retry" "4000 5000 4900 5000"
run_case secondary-short-retry-after 0 "5" "budget is available" \
	"core-secondary-short-retry" "4000 5000 4900 5000" -- --max-wait 15
run_case secondary-after-other-primary-exhaustion 0 "605" "budget is available" \
	"graphql-secondary-after-core-exhaustion" "4000 5000 4900 5000"
run_case secondary-persistent 1 "60 120" "secondary rate limit persisted" "core-secondary-default"
run_case secondary-at-deadline 1 "" "secondary rate limit does not recover within 0 s" \
	"core-secondary-default" -- --max-wait 0
run_case secondary-retry-after-beyond-deadline 1 "" "secondary rate limit does not recover within 100 s" \
	"core-secondary-retry" -- --max-wait 100
run_case secondary-primary-also-exhausted 0 "305" "budget is available" \
	"core-secondary-zero-header" "4000 5000 4900 5000"
run_case secondary-invalid-retry-after 1 "" "Could not validate GitHub rate-limit backoff" "core-secondary-bad-retry"
run_case secondary-recovery-unreadable 1 "60" "Could not confirm recovery from the GitHub secondary rate limit" \
	"core-secondary-default" "fail"

# A reset already in the past still pauses briefly instead of spinning.
run_case stale-reset 0 "5" "budget is available" \
	"0 900 4900 900" "5000 4500 4900 4500"

# Still exhausted after the reset: wait again, bounded by the deadline.
run_case exhausted-again 0 "605 3605" "budget is available" \
	"0 1600 4900 1600" "0 5205 4900 5205" "5000 9000 4900 9000" -- --max-wait 5000

# The reset lies beyond the maximum wait: fail, and do not sleep at all.
run_case beyond-max-wait 1 "" "does not return within 60 s" \
	"0 1600 4900 1600" -- --max-wait 60

# An unreadable budget never stops a release: three attempts, then a warning and exit 0.
run_case unreadable 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"fail"
run_case malformed 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"garbage"
for invalid in core-failed-full-output graphql-failed-full-output graphql-partial \
	graphql-wrong-resource graphql-duplicate-header graphql-missing-header graphql-other-error; do
	run_case "${invalid}" 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" "${invalid}"
done
run_case graphql-partial-then-exhausted 0 "10 895" "budget is available" \
	"graphql-partial" "4000 1600 4900 1100 0 1900" "4000 5200 4900 1100 5000 5200"

# Invalid API numbers must use the unreadable-response path, never claim available
# capacity or enter Bash arithmetic with scientific notation or oversized values.
run_case zero-core-reset 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"0 0 50 1700000000"
run_case zero-graphql-reset 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"300 1700000000 0 0"
run_case oversized-core-reset 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"0 1e20 50 1700000000"
run_case oversized-graphql-reset 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"300 1700000000 0 1e20"
run_case oversized-core-remaining 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"1e20 1700000000 50 1700000000"
run_case oversized-graphql-remaining 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"300 1700000000 1e20 1700000000"
run_case fractional-number 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"300.5 1700000000 50 1700000000"
run_case negative-number 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"-1 1700000000 50 1700000000"
run_case numeric-string 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	'"300" 1700000000 50 1700000000'
run_case null-number 0 "10 10" "Could not read the GitHub request budget (3 attempt(s))" \
	"300 1700000000 null 1700000000"

# A retry never outlasts --max-wait: with the deadline already reached, a failed read gives up at
# once, both on the first read and after a wait for the reset has used the time up.
run_case unreadable-at-deadline 0 "" "Could not read the GitHub request budget (1 attempt(s))" \
	"fail" -- --max-wait 5
run_case stalled-core-read 0 "" "Could not read the GitHub request budget (1 attempt(s))" \
	"core-hangs" -- --max-wait 0
run_case stalled-graphql-read 0 "" "Could not read the GitHub request budget (1 attempt(s))" \
	"graphql-hangs" -- --max-wait 0
run_case unreadable-after-reset-wait 0 "605" "Could not read the GitHub request budget (1 attempt(s))" \
	"0 1600 4900 1600" "fail" -- --max-wait 610
# With time left, the retry still happens.
run_case unreadable-with-time-left 0 "10" "Could not read the GitHub request budget (2 attempt(s))" \
	"fail" -- --max-wait 15
# One failed read does not skip the wait when the next read shows exhaustion.
run_case recovers-after-failed-read 0 "10 595" "budget is available" \
	"fail" "0 1600 4900 1600" "5000 5200 4900 5200"

# Custom floors are honoured.
run_case custom-floor 0 "105" "REST 900 of at least 1000" \
	"900 1100 4900 1100" "5000 4700 4900 4700" -- --min-core 1000

# Usage errors.
run_case bad-number 2 "" "--max-wait needs a whole number" "4000 5000 4900 5000" -- --max-wait soon
run_case missing-value 2 "" "--min-core needs a whole number" "4000 5000 4900 5000" -- --min-core
run_case unknown-argument 2 "" "unknown argument --force" "4000 5000 4900 5000" -- --force

if [[ "${failures}" -ne 0 ]]; then
	printf '%s case(s) failed\n' "${failures}" >&2
	exit 1
fi
printf 'wait-for-github-api-budget: all cases passed\n'
