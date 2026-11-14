#!/usr/bin/env bash
# summarize-mutation-report.sh — turn one gremlins JSON report into a mutation-score summary.
#
# WHY (ksail#7849): agents write most of KSail's code and the tests that check it, so a green
# suite with good line coverage can still assert very little. The scheduled mutation pilot runs
# gremlins on packages where a silent regression is costly; this script publishes each package's
# score and its surviving mutants for triage.
#
# It fails closed: a missing or malformed report, or one in which gremlins examined no mutant at
# all, is an error, never a green run with an empty score.
#
# Usage: summarize-mutation-report.sh <package> <report.json>
# Prints a Markdown section (for $GITHUB_STEP_SUMMARY) on stdout.
# Exit codes: 0 summarized, 1 unusable or vacuous report, 2 usage error.

set -euo pipefail

if (($# != 2)); then
	echo "usage: summarize-mutation-report.sh <package> <report.json>" >&2
	exit 2
fi

package="$1"
report="$2"

if [[ ! -s "${report}" ]]; then
	echo "mutation report for ${package} is missing or empty: ${report}" >&2
	exit 1
fi

counts="$(jq -er '
	[.mutants_killed, .mutants_lived, .mutants_not_covered, .mutants_not_viable,
	 (.mutants_timed_out // 0), .test_efficacy, .mutations_coverage, .elapsed_time]
	| if all(type == "number") then . else error("non-numeric field") end
	# Format here, not with printf, so the decimal separator never follows the runner locale.
	| .[0:5] + [(.[5] * 100 | round / 100), (.[6] * 100 | round / 100), (.[7] | round)]
	| map(tostring) | join(" ")
' "${report}" 2>/dev/null)" || {
	echo "mutation report for ${package} is not a gremlins JSON report: ${report}" >&2
	exit 1
}

read -r killed lived not_covered not_viable timed_out efficacy coverage elapsed <<<"${counts}"

examined=$((killed + lived + timed_out))
if ((examined == 0)); then
	echo "gremlins examined no mutants in ${package}; refusing a vacuous score" >&2
	exit 1
fi

# shellcheck disable=SC2016 # the backticks are Markdown, not a command substitution
printf '### Mutation score: `%s`\n\n' "${package}"
printf '| Killed | Lived | Timed out | Not covered | Not viable | Test efficacy | Mutant coverage | Elapsed |\n'
printf '|---:|---:|---:|---:|---:|---:|---:|---:|\n'
printf '| %s | %s | %s | %s | %s | %s%% | %s%% | %ss |\n\n' \
	"${killed}" "${lived}" "${timed_out}" "${not_covered}" "${not_viable}" \
	"${efficacy}" "${coverage}" "${elapsed}"

if ((lived == 0)); then
	printf 'No mutant survived.\n'
	exit 0
fi

printf '<details><summary>%s surviving mutants to triage</summary>\n\n' "${lived}"
printf '| File | Line | Mutation |\n|---|---:|---|\n'
jq -r '
	.files[]? | .file_name as $file | .mutations[]? | select(.status == "LIVED")
	| "| `\($file)` | \(.line) | \(.type) |"
' "${report}"
printf '\n</details>\n'
