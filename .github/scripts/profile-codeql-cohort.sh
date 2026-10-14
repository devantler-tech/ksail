#!/usr/bin/env bash
# Exactly one fixed six-job cohort; never retry or choose by extraction outcome.
set -euo pipefail
[[ $# == 2 && "$1" =~ ^[a-f0-9]{40}$ ]]
sha="$1" directory="$2"
: "${GITHUB_RUN_ID:?The originating workflow run must be explicit}"
: "${GITHUB_RUN_ATTEMPT:?The originating attempt must be explicit}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
reports=("$directory"/*/report.json)
[[ ${#reports[@]} == 6 ]] || {
	printf 'Expected all six cohort reports.\n' >&2
	exit 1
}

report_path() {
	printf '%s/codeql-go-profile-%s-attempt%s-swap%s-replica%s/report.json' \
		"$directory" "$sha" "$GITHUB_RUN_ATTEMPT" "$1" "$2"
}

# Bind all six slots before selection. Missing, duplicate or misnamed artifacts
# cannot be substituted by an otherwise attractive result.
for replica in 0 1 2; do
	for arm in 0 8; do
		report="$(report_path "$arm" "$replica")"
		[[ -f "$report" && ! -L "$report" && ! -L "${report%/*}" ]]
		jq -se --arg sha "$sha" --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
			--argjson arm "$arm" --argjson replica "$replica" \
			'length==1 and (.[0]|.sourceSha==$sha and .workflowRunId==$run and .workflowRunAttempt==$attempt and
        .swapPreparation.requestedAddedSwapGiB==$arm and .replica==$replica)' "$report" >/dev/null
	done
done

# Freeze the metadata-only selection before inspecting success or measurements.
jq -L "$script_dir" -s -f "$script_dir/profile-codeql-cohort-select.jq" "${reports[@]}" >"$scratch/selection.json"
for replica in 0 1 2; do
	for arm in 0 8; do
		bash "$script_dir/profile-codeql-compare.sh" validate "$sha" "$arm" "$(report_path "$arm" "$replica")" >&2
	done
done
# Coverage disagreement in an unselected successful observation is still a
# failure, never something selection can hide. Failed baselines stay explicit.
jq -se '[.[]|select(.profileExitCode==0)|.nativeFiles]|unique|length==1' "${reports[@]}" >/dev/null
if ! jq -e '.baseline!=null and .treatment!=null' "$scratch/selection.json" >/dev/null; then
	jq -s '{collectionValid:true,comparableCoverage:false,outcome:"inconclusive-no-matching-environment",
    selection:.[0],observations:.[1:],baseline:null,treatment:null,projects:null}' \
		"$scratch/selection.json" "${reports[@]}"
	printf 'Inconclusive: no matching environment in the fixed cohort.\n' >&2
	exit 1
fi
baseline_replica="$(jq -er '.baseline.replica' "$scratch/selection.json")"
treatment_replica="$(jq -er '.treatment.replica' "$scratch/selection.json")"
bash "$script_dir/profile-codeql-compare.sh" compare "$sha" \
	"$(report_path 0 "$baseline_replica")" "$(report_path 8 "$treatment_replica")" >"$scratch/pair.json"
jq -s '.[0] as $selection | .[1] + {selection:$selection,observations:.[2:]}' \
	"$scratch/selection.json" "$scratch/pair.json" "${reports[@]}"
