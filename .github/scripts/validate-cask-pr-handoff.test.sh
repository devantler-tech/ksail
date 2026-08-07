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
	local filter="${4:-.}" prepared="${5:-false}" ready="${6:-false}" merged="${7:-false}"
	local evidence="${tmp_dir}/${name}.json" output status
	local args=(--evidence "${evidence}" --tap devantler-tech/homebrew-tap --cask-name ksail --tag v7.166.1)

	jq "${filter}" "${fixture}" >"${evidence}"
	if [[ "${prepared}" == true ]]; then
		args+=(--prepared)
	fi
	if [[ "${ready}" == true ]]; then
		args+=(--ready)
	fi
	if [[ "${merged}" == true ]]; then
		args+=(--merged)
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
run_case wrong-branch 1 'head branch must exactly equal goreleaser/ksail' '.pr.head.ref = "feature/untrusted"'
run_case legacy-versioned-branch 1 'head branch must exactly equal goreleaser/ksail' '.pr.head.ref = "goreleaser/ksail-v7.166.1"'
run_case wrong-base 1 'base branch must exactly equal main' '.pr.base.ref = "release"'
run_case invalid-head 1 'head SHA is missing or invalid' '.pr.head.sha = "short"'
run_case missing-files 1 'changed-file evidence is missing' 'del(.files)'
run_case extra-file 1 'only Casks/ksail.rb may change' '.files += [{"filename":"README.md"}]'
run_case wrong-file 1 'only Casks/ksail.rb may change' '.files = [{"filename":"Casks/other.rb"}]'
run_case missing-head-content 1 'cask head-content evidence is missing' 'del(.headFile)'
run_case wrong-head-content-path 1 'cask head-content evidence is missing' '.headFile.path = "Casks/other.rb"'
# base64 of a cask pinning 7.160.0 — a previous release the evergreen branch rewrite never replaced.
run_case stale-head-version 1 'cask at head must pin version 7.166.1' '.headFile.content = "Y2FzayAia3NhaWwiIGRvCiAgdmVyc2lvbiAiNy4xNjAuMCIKICBzaGEyNTYgIjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAiCgogIHVybCAiaHR0cHM6Ly9naXRodWIuY29tL2RldmFudGxlci10ZWNoL2tzYWlsL3JlbGVhc2VzL2Rvd25sb2FkL3Y3LjE2MC4wL2tzYWlsXzcuMTYwLjBfZGFyd2luX2FybTY0LnRhci5neiIKZW5kCg=="'
run_case missing-release-assets 1 'release-asset digest evidence is missing' 'del(.releaseAssets)'
run_case empty-release-assets 1 'release-asset digest evidence is missing' '.releaseAssets = []'
# Same tag re-run: version already matches, but the published assets carry different
# digests than the stale cask's sha256 — the handoff must block.
run_case stale-cask-sha 1 'does not match any published release asset digest' '.releaseAssets[0].digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"'
# base64 of a cask with a version but no sha256 stanza at all.
run_case no-cask-sha 1 'cask at head must pin at least one sha256' '.headFile.content = "Y2FzayAia3NhaWwiIGRvCiAgdmVyc2lvbiAiNy4xNjYuMSIKCiAgdXJsICJodHRwczovL2dpdGh1Yi5jb20vZGV2YW50bGVyLXRlY2gva3NhaWwvcmVsZWFzZXMvZG93bmxvYWQvdjcuMTY2LjEva3NhaWxfNy4xNjYuMV9kYXJ3aW5fYXJtNjQudGFyLmd6IgplbmQK"'
run_case empty-head-content 1 'cask head content must not be empty' '.headFile.content = ""'
run_case invalid-base64-content 1 'cask head content is not valid base64' '.headFile.content = "%%%not-base64%%%"'
run_case missing-title 1 'title must exactly equal chore(cask): update ksail to v7.166.1' '.pr.title = "Brew cask update"' true
run_case missing-label-inventory 1 'available-label inventory is missing' 'del(.availableLabels)' true
run_case malformed-label-inventory 1 'available-label inventory is malformed' '.availableLabels = [{}] | .pr.labels = []' true
run_case missing-label 1 'available label is missing from PR: dependencies' '.pr.labels = [{"name":"automation"}]' true
run_case unavailable-label 0 'PASS: generated cask PR identity, scope, and metadata are valid' '.availableLabels = [{"name":"automation"}] | .pr.labels = [{"name":"automation"}]' true

# --ready revalidates an already-handed-off (promoted, non-draft) PR on an idempotent rerun: it must
# pass on a non-draft PR and fail on one still in draft, the inverse of the default draft requirement.
run_case ready-nondraft 0 'PASS: generated cask PR identity, scope, and metadata are valid' '.pr.draft = false' true true
run_case ready-still-draft 1 'already-handed-off PR must be marked ready for review' '.' true true

# A newer release can update an evergreen branch while the previous release's PR is still open. The
# tap then merges the newer cask through the older-titled PR. A merged handoff is valid only when the
# immutable PR file blob is still the current blob on tap main and that current content proves the
# requested version and published asset digest. Its title is deliberately not part of that proof
# because it records the earlier release that opened the PR.
merged_fixture='.
  | .pr.state = "closed"
  | .pr.draft = false
  | .pr.merged = true
  | .pr.merged_at = "2026-07-18T19:58:28Z"
  | .pr.merge_commit_sha = "4444444444444444444444444444444444444444"
  | .pr.title = "chore(cask): update ksail to v7.166.0"
  | .pr.base.repo.full_name = "devantler-tech/homebrew-tap"
  | .files[0].sha = "2222222222222222222222222222222222222222"
  | .headFile.sha = "2222222222222222222222222222222222222222"
  | .mergeFile = {
      "path": "Casks/ksail.rb",
      "sha": "2222222222222222222222222222222222222222",
      "content": .headFile.content
    }
  | .mainRef = {"sha": "5555555555555555555555555555555555555555"}
  | .mainFile = {
      "path": "Casks/ksail.rb",
      "sha": "2222222222222222222222222222222222222222",
      "content": .headFile.content
    }
  | .mergeComparison = {
      "status": "ahead",
      "behind_by": 0,
      "merge_base_commit": {"sha": "4444444444444444444444444444444444444444"}
    }
'
run_case coalesced-merged 0 'PASS: merged generated cask PR identity and scope are valid' \
	"${merged_fixture}" \
	false false true
run_case merged-without-merge 1 'merged PR must have a merged_at timestamp' \
	"${merged_fixture} | .pr.merged_at = null" false false true
run_case merged-flag-false 1 'merged PR must be marked merged' \
	"${merged_fixture} | .pr.merged = false" false false true
run_case merged-invalid-commit 1 'merge commit SHA is missing or invalid' \
	"${merged_fixture} | .pr.merge_commit_sha = \"short\"" false false true
run_case merged-mode-open 1 'merged PR state must be closed' \
	"${merged_fixture} | .pr.state = \"open\"" false false true
run_case merged-wrong-author 1 'PR author must be devantler' \
	"${merged_fixture} | .pr.user.login = \"someone-else\"" \
	false false true
run_case merged-wrong-branch 1 'head branch must exactly equal goreleaser/ksail' \
	"${merged_fixture} | .pr.head.ref = \"feature/untrusted\"" \
	false false true
run_case merged-wrong-base-repository 1 'base repository must exactly equal devantler-tech/homebrew-tap' \
	"${merged_fixture} | .pr.base.repo.full_name = \"someone-else/homebrew-tap\"" false false true
run_case merged-missing-main 1 'current-main cask evidence is missing' \
	"${merged_fixture} | del(.mainFile)" false false true
run_case merged-wrong-main-path 1 'current-main cask evidence is missing' \
	"${merged_fixture} | .mainFile.path = \"Casks/other.rb\"" false false true
run_case merged-missing-pr-blob 1 'merged PR cask blob SHA is missing or invalid' \
	"${merged_fixture} | del(.files[0].sha)" false false true
run_case merged-head-blob-mismatch 1 'merged PR head cask blob must match its changed-file blob' \
	"${merged_fixture} | .headFile.sha = \"3333333333333333333333333333333333333333\"" false false true
run_case merged-missing-result 1 'merge-result cask evidence is missing' \
	"${merged_fixture} | del(.mergeFile)" false false true
run_case merged-result-blob-mismatch 1 'merge-result cask blob must match the merged PR changed-file blob' \
	"${merged_fixture} | .mergeFile.sha = \"3333333333333333333333333333333333333333\"" false false true
run_case merged-invalid-main-ref 1 'pinned main SHA is missing or invalid' \
	"${merged_fixture} | .mainRef.sha = \"short\"" false false true
run_case merged-main-blob-mismatch 1 'merged PR cask blob must still be current on main' \
	"${merged_fixture} | .mainFile.sha = \"3333333333333333333333333333333333333333\"" false false true
run_case merged-empty-main-content 1 'current-main cask content must not be empty' \
	"${merged_fixture} | .mainFile.content = \"\"" false false true
run_case merged-missing-ancestry 1 'merge ancestry evidence is missing or invalid' \
	"${merged_fixture} | del(.mergeComparison)" false false true
run_case merged-diverged-main 1 'merge commit must be an ancestor of pinned main' \
	"${merged_fixture} | .mergeComparison.status = \"diverged\"" false false true
run_case merged-behind-main 1 'merge commit must not be behind the pinned-main merge base' \
	"${merged_fixture} | .mergeComparison.behind_by = 1" false false true
run_case merged-wrong-merge-base 1 'pinned main must descend from the merge commit' \
	"${merged_fixture} | .mergeComparison.merge_base_commit.sha = \"6666666666666666666666666666666666666666\"" false false true
# base64 of the otherwise-valid cask changed to version 7.160.0: current main must not merely carry
# the merged blob, it must still prove the exact release this CD run is handing off.
run_case merged-stale-main-version 1 'cask on main must pin version 7.166.1' \
	"${merged_fixture} | .mainFile.content = \"Y2FzayBcImtzYWlsXCIgZG8KICB2ZXJzaW9uIFwiNy4xNjAuMFwiCiAgc2hhMjU2IFwiMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMFwiCmVuZAo=\"" \
	false false true

printf 'All %d cask PR handoff cases passed.\n' "${pass_count}"
