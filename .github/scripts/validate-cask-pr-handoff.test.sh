#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
validator="${script_dir}/validate-cask-pr-handoff.sh"
fixture="${repo_root}/.github/fixtures/cask-pr-handoff/valid.json"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
pass_count=0

run_case() {
	local name="$1" expected_status="$2" expected_output="$3"
	local filter="${4:-.}" prepared="${5:-false}"
	local evidence="${tmp_dir}/${name}.json" output status
	local args=(--evidence "${evidence}" --tap devantler-tech/homebrew-tap --cask-name ksail --tag v7.166.1)

	jq "${filter}" "${fixture}" >"${evidence}"
	if [[ "${prepared}" == true ]]; then
		args+=(--prepared)
	fi

	set +e
	output="$(${validator} "${args[@]}" 2>&1)"
	status=$?
	set -e

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' \
			"${name}" "${expected_status}" "${status}" "${output}" >&2
		return 1
	fi
	if [[ "${output}" != *"${expected_output}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' \
			"${name}" "${expected_output}" "${output}" >&2
		return 1
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${name}"
}

run_case valid-draft 0 'PASS: generated cask PR identity and scope are valid'
run_case prepared 0 'PASS: generated cask PR identity, scope, and metadata are valid' '.' true
run_case closed 1 'PR state must be open' '.pr.state = "closed"'
run_case promoted 1 'PR must remain a draft for maintainer promotion' '.pr.draft = false'
run_case wrong-author 1 'PR author must be devantler' '.pr.user.login = "someone-else"'
run_case cross-repository 1 'head repository must exactly equal devantler-tech/homebrew-tap' '.pr.head.repo.full_name = "someone-else/homebrew-tap"'
run_case missing-head-repository 1 'head repository must exactly equal devantler-tech/homebrew-tap' 'del(.pr.head.repo)'
run_case wrong-branch 1 'head branch must exactly equal goreleaser/ksail-v7.166.1' '.pr.head.ref = "feature/untrusted"'
run_case wrong-base 1 'base branch must exactly equal main' '.pr.base.ref = "release"'
run_case invalid-head 1 'head SHA is missing or invalid' '.pr.head.sha = "short"'
run_case missing-files 1 'changed-file evidence is missing' 'del(.files)'
run_case extra-file 1 'only Casks/ksail.rb may change' '.files += [{"filename":"README.md"}]'
run_case wrong-file 1 'only Casks/ksail.rb may change' '.files = [{"filename":"Casks/other.rb"}]'
run_case missing-title 1 'title must exactly equal chore(cask): update ksail to v7.166.1' '.pr.title = "Brew cask update"' true
run_case missing-label-inventory 1 'available-label inventory is missing' 'del(.availableLabels)' true
run_case malformed-label-inventory 1 'available-label inventory is malformed' '.availableLabels = [{}] | .pr.labels = []' true
run_case missing-label 1 'available label is missing from PR: dependencies' '.pr.labels = [{"name":"automation"}]' true
run_case unavailable-label 0 'PASS: generated cask PR identity, scope, and metadata are valid' '.availableLabels = [{"name":"automation"}] | .pr.labels = [{"name":"automation"}]' true

printf 'All %d cask PR handoff cases passed.\n' "${pass_count}"
