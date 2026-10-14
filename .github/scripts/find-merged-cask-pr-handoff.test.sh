#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
finder="${script_dir}/find-merged-cask-pr-handoff.sh"
fake_bin="${repo_root}/.github/fixtures/cask-pr-handoff/fake-bin"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
pass_count=0

run_case() {
	local scenario="$1" expected_status="$2" expected_output="$3"
	shift 3
	local case_dir="${tmp_dir}/${scenario}-${pass_count}" output status
	mkdir -p "${case_dir}/evidence" "${case_dir}/state"
	set +e
	output="$(PATH="${fake_bin}:${PATH}" \
		FAKE_GH_SCENARIO="${scenario}" \
		FAKE_GH_STATE_DIR="${case_dir}/state" \
		"${finder}" \
		--tap devantler-tech/homebrew-tap \
		--cask-name ksail \
		--source-repo devantler-tech/ksail \
		--tag v7.166.1 \
		--output-dir "${case_dir}/evidence" "$@" 2>&1)"
	status=$?
	set -e

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' \
			"${scenario}" "${expected_status}" "${status}" "${output}" >&2
		return 1
	fi
	if [[ "${expected_status}" -eq 0 && "${output}" != "${expected_output}" ]]; then
		printf 'FAIL: %s: expected exact successful output %q, got:\n%s\n' \
			"${scenario}" "${expected_output}" "${output}" >&2
		return 1
	fi
	if [[ "${expected_status}" -ne 0 && "${output}" != *"${expected_output}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' \
			"${scenario}" "${expected_output}" "${output}" >&2
		return 1
	fi
	if [[ "${expected_status}" -eq 0 && ! -s "${case_dir}/evidence/merged-42.json" ]]; then
		printf 'FAIL: %s: successful lookup did not preserve validated evidence\n' "${scenario}" >&2
		return 1
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${scenario}"
}

# The fake merged PR deliberately carries the older v7.166.0 title while its immutable merge and
# pinned-main cask content prove v7.166.1. The title-independent full validator must accept it.
run_case merged 0 '42'
# `gh api --paginate --slurp` must expose a candidate that is absent from page one.
run_case merged-later-page 0 '42'
# A rejected newer candidate must not contaminate stdout when an older candidate has the same
# current-main cask and validates successfully. CD captures both streams into the PR-number variable.
run_case merged-newest-invalid 0 '42'
run_case merged-wrong-list-author 1 'no trusted merged cask PR'
run_case merged-closed-unmerged 1 'no trusted merged cask PR'
run_case merged-list-failure 1 'could not enumerate merged cask PRs'
run_case merged-main-file-failure 1 'no valid merged cask PR'
run_case merged-moving-main 1 'no valid merged cask PR'

# Discovery can omit the selected PR, or finish after that PR merges. The named
# read must exercise the same collector/validator and retain its immutable proof.
run_case merged-known-not-listed 0 '42' --pr 42
run_case merged-known-late 0 '42' --pr 42
run_case merged-list-failure 0 '42' --pr 42
for scenario in merged-main-file-failure merged-moving-main merged-merge-file-failure \
	merged-main-ref-failure merged-malformed-main-file merged-comparison-failure; do
	run_case "${scenario}" 1 'no valid merged cask PR' --pr 42
done
run_case merged-known-untrusted 1 'no valid merged cask PR' --pr 42
run_case merged-known-open 1 'no valid merged cask PR' --pr 42
run_case merged-known-extra-file 1 'no valid merged cask PR' --pr 42
run_case merged-known-wrong-digest 1 'no valid merged cask PR' --pr 42
run_case merged-known-pr-failure 1 'no valid merged cask PR' --pr 42

# An invalid named candidate must not contaminate the number when an independently
# validated historical candidate still proves this release on current main.
run_case merged-newest-invalid 0 '42' --pr 43

printf 'All %d merged cask handoff finder cases passed.\n' "${pass_count}"
