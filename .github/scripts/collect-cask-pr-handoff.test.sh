#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
collector="${script_dir}/collect-cask-pr-handoff.sh"
validator="${script_dir}/validate-cask-pr-handoff.sh"
fake_bin="${repo_root}/.github/fixtures/cask-pr-handoff/fake-bin"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -x "${fake_bin}/gh" ]]; then
	printf 'FAIL: tracked fake gh executable is missing: %s\n' "${fake_bin}/gh" >&2
	exit 1
fi

run_collector() {
	local scenario="$1" output="$2" include_main="${3:-false}"
	local state_dir="${tmp_dir}/state-${scenario}"
	local args=(
		--tap devantler-tech/homebrew-tap
		--pr 42
		--source-repo devantler-tech/ksail
		--tag v7.166.1
		--output "${output}"
	)
	rm -rf "${state_dir}"
	mkdir -p "${state_dir}"
	if [[ "${include_main}" == true ]]; then
		args+=(--include-main)
	fi
	PATH="${fake_bin}:${PATH}" FAKE_GH_SCENARIO="${scenario}" FAKE_GH_STATE_DIR="${state_dir}" \
		"${collector}" "${args[@]}"
}

valid="${tmp_dir}/valid.json"
run_collector valid "${valid}"
jq -e '
  .pr.head.repo.full_name == "devantler-tech/homebrew-tap"
  and (.files | map(.filename)) == ["Casks/ksail.rb"]
  and (.availableLabels | map(.name)) == ["automation", "dependencies"]
  and (.releaseAssets | map(.name)) == ["ksail_7.166.1_darwin_arm64.tar.gz"]
  and (.releaseAssets[0].digest | startswith("sha256:"))
  and .headFile.path == "Casks/ksail.rb"
  and (.headFile.content | type == "string" and length > 0)
' "${valid}" >/dev/null
"${validator}" \
	--evidence "${valid}" \
	--tap devantler-tech/homebrew-tap \
	--cask-name ksail \
	--tag v7.166.1 \
	--prepared >/dev/null
printf 'PASS: collector-to-validator round trip\n'

paginated="${tmp_dir}/paginated.json"
run_collector paginated-files "${paginated}"
jq -e '
  (.files | map(.filename)) == ["Casks/ksail.rb", "README.md"]
  and .headFile == null
' "${paginated}" >/dev/null
if "${validator}" \
	--evidence "${paginated}" \
	--tap devantler-tech/homebrew-tap \
	--cask-name ksail \
	--tag v7.166.1 >/dev/null 2>&1; then
	printf 'FAIL: validator accepted an extra file from a later API page\n' >&2
	exit 1
fi
printf 'PASS: paginated extra file remains visible and blocks\n'

# A coalesced release is merged through an older-titled evergreen PR. Collection must pin main,
# capture the immutable merge-result cask, prove merge ancestry, and re-read main so the validator
# can bind the trusted merged PR to the cask that is actually current.
merged="${tmp_dir}/merged.json"
run_collector merged "${merged}" true
jq -e '
  .pr.merged == true
  and .pr.merge_commit_sha == "4444444444444444444444444444444444444444"
  and .files[0].sha == "2222222222222222222222222222222222222222"
  and .mergeFile.sha == "2222222222222222222222222222222222222222"
  and .mainRef.sha == "5555555555555555555555555555555555555555"
  and .mainFile.sha == "2222222222222222222222222222222222222222"
  and .mergeComparison.status == "ahead"
  and .mergeComparison.behind_by == 0
  and .mergeComparison.merge_base_commit.sha == "4444444444444444444444444444444444444444"
' "${merged}" >/dev/null
"${validator}" \
	--evidence "${merged}" \
	--tap devantler-tech/homebrew-tap \
	--cask-name ksail \
	--tag v7.166.1 \
	--merged >/dev/null
printf 'PASS: merged collector-to-validator round trip\n'

if run_collector valid "${tmp_dir}/open-with-main.json" true >/dev/null 2>&1; then
	printf 'FAIL: collector accepted --include-main for an open PR\n' >&2
	exit 1
fi
printf 'PASS: merged collection rejects an open PR\n'

for scenario in pr-failure files-failure labels-failure malformed-files malformed-labels contents-failure malformed-contents release-failure malformed-release; do
	if run_collector "${scenario}" "${tmp_dir}/${scenario}.json" >/dev/null 2>&1; then
		printf 'FAIL: collector accepted %s\n' "${scenario}" >&2
		exit 1
	fi
	printf 'PASS: collector fails closed on %s\n' "${scenario}"
done

for scenario in merged-main-ref-failure merged-merge-file-failure merged-main-file-failure merged-malformed-main-file merged-comparison-failure merged-moving-main; do
	if run_collector "${scenario}" "${tmp_dir}/${scenario}.json" true >/dev/null 2>&1; then
		printf 'FAIL: merged collector accepted %s\n' "${scenario}" >&2
		exit 1
	fi
	printf 'PASS: merged collector fails closed on %s\n' "${scenario}"
done

printf 'All cask PR handoff collector cases passed.\n'
