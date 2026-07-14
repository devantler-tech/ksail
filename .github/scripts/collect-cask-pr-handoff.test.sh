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
	local scenario="$1" output="$2"
	PATH="${fake_bin}:${PATH}" FAKE_GH_SCENARIO="${scenario}" \
		"${collector}" \
		--tap devantler-tech/homebrew-tap \
		--pr 42 \
		--source-repo devantler-tech/ksail \
		--tag v7.166.1 \
		--output "${output}"
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

for scenario in pr-failure files-failure labels-failure malformed-files malformed-labels contents-failure malformed-contents release-failure malformed-release; do
	if run_collector "${scenario}" "${tmp_dir}/${scenario}.json" >/dev/null 2>&1; then
		printf 'FAIL: collector accepted %s\n' "${scenario}" >&2
		exit 1
	fi
	printf 'PASS: collector fails closed on %s\n' "${scenario}"
done

printf 'All cask PR handoff collector cases passed.\n'
