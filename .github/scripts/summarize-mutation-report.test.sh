#!/usr/bin/env bash
# Tests for summarize-mutation-report.sh (ksail#7849): a real score is summarized with its
# survivors, and every unusable or vacuous report fails instead of printing a green score.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
summarize="${script_dir}/summarize-mutation-report.sh"
work="$(mktemp -d)"
trap 'rm -rf -- "${work}"' EXIT

failures=0

fail() {
	echo "FAIL: $*" >&2
	failures=$((failures + 1))
}

expect_status() {
	local want="$1" name="$2"
	shift 2

	local status=0
	"$@" >"${work}/out" 2>"${work}/err" || status=$?
	if ((status != want)); then
		fail "${name}: exit ${status}, want ${want} ($(cat "${work}/err"))"
	fi
}

cat >"${work}/scored.json" <<'JSON'
{"go_module":"github.com/devantler-tech/ksail/v7","test_efficacy":75.0,"mutations_coverage":80.0,
 "mutants_total":5,"mutants_killed":3,"mutants_lived":1,"mutants_not_viable":0,"mutants_not_covered":1,
 "elapsed_time":12.3,"files":[{"file_name":"pkg/cli/lifecycle/simple.go","mutations":[
 {"line":10,"column":2,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
 {"line":42,"column":7,"type":"CONDITIONALS_BOUNDARY","status":"LIVED"}]}]}
JSON

expect_status 0 "scored report" "${summarize}" pkg/cli/lifecycle "${work}/scored.json"
grep -q '| 3 | 1 | 0 | 1 | 0 | 75% | 80% | 12s |' "${work}/out" ||
	fail "scored report: counts row missing: $(cat "${work}/out")"
# shellcheck disable=SC2016 # the backticks are Markdown
grep -q '| `pkg/cli/lifecycle/simple.go` | 42 | CONDITIONALS_BOUNDARY |' "${work}/out" ||
	fail "scored report: surviving mutant not listed"
if grep -q '| 10 |' "${work}/out"; then
	fail "scored report: a killed mutant was listed as a survivor"
fi

jq '.mutants_lived = 0 | .files[0].mutations |= map(select(.status != "LIVED"))' \
	"${work}/scored.json" >"${work}/clean.json"
expect_status 0 "no survivors" "${summarize}" pkg/cli/lifecycle "${work}/clean.json"
grep -q 'No mutant survived.' "${work}/out" || fail "no survivors: message missing"

jq '.mutants_killed = 0 | .mutants_lived = 0 | .mutants_not_covered = 5' \
	"${work}/scored.json" >"${work}/vacuous.json"
expect_status 1 "vacuous report" "${summarize}" pkg/cli/lifecycle "${work}/vacuous.json"
grep -q 'examined no mutants' "${work}/err" || fail "vacuous report: wrong reason"

: >"${work}/empty.json"
expect_status 1 "empty report" "${summarize}" pkg/cli/lifecycle "${work}/empty.json"
expect_status 1 "missing report" "${summarize}" pkg/cli/lifecycle "${work}/absent.json"

echo '{"mutants_killed":"3"}' >"${work}/malformed.json"
expect_status 1 "malformed report" "${summarize}" pkg/cli/lifecycle "${work}/malformed.json"
grep -q 'not a gremlins JSON report' "${work}/err" || fail "malformed report: wrong reason"

expect_status 2 "usage" "${summarize}" only-one-argument

if ((failures > 0)); then
	echo "${failures} check(s) failed" >&2
	exit 1
fi

echo "summarize-mutation-report: all checks passed"
