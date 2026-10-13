#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
wait_script="${script_dir}/wait-for-github-api-budget.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

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
[[ "$*" == "api rate_limit" ]] || { printf 'unexpected fake gh invocation: %s\n' "$*" >&2; exit 2; }
reads="$(cat "${state_dir}/reads")"
printf '%s\n' "$((reads + 1))" >"${state_dir}/reads"
total="$(wc -l <"${state_dir}/responses" | tr -d ' ')"
line=$((reads + 1))
[[ "${line}" -gt "${total}" ]] && line="${total}"
response="$(sed -n "${line}p" "${state_dir}/responses")"
case "${response}" in
fail) exit 1 ;;
garbage) printf '%s\n' '{"resources":{"core":{"remaining":"many","reset":null}}}' ;;
*)
	read -r core_remaining core_reset graphql_remaining graphql_reset <<<"${response}"
	printf '{"resources":{"core":{"remaining":%s,"reset":%s},"graphql":{"remaining":%s,"reset":%s}}}\n' \
		"${core_remaining}" "${core_reset}" "${graphql_remaining}" "${graphql_reset}"
	;;
esac
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
		bash "${wait_script}" "$@" 2>&1)" || status=$?
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
run_case both-exhausted 0 "905" "budget is available" \
	"0 1100 0 1900" "5000 4700 5000 5500"

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
# A retry never outlasts --max-wait: with the deadline already reached, a failed read gives up at
# once, both on the first read and after a wait for the reset has used the time up.
run_case unreadable-at-deadline 0 "" "Could not read the GitHub request budget (1 attempt(s))" \
	"fail" -- --max-wait 5
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
