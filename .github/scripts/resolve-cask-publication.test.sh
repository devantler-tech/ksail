#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
resolver="${script_dir}/resolve-cask-publication.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

# The fake answers only the three reads the resolver is allowed to make. Any other call fails the
# case, so a resolver that writes, or reads anything else, cannot pass.
cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

command_line="$*"
cask_body() {
	if [[ "$1" == __missing__ ]]; then
		printf 'cask "x" do\n  sha256 "abc"\nend\n'
	elif [[ "$1" == __twice__ ]]; then
		printf 'cask "x" do\n  version "1.0.0"\n  version "2.0.0"\nend\n'
	else
		printf 'cask "x" do\n  version "%s"\n  sha256 "abc"\nend\n' "$1"
	fi
}

case "${command_line}" in
"api -H Accept: application/vnd.github.raw repos/devantler-tech/homebrew-tap/contents/Casks/ksail.rb")
	[[ "${FAKE_CLI_VERSION}" == __error__ ]] && exit 1
	cask_body "${FAKE_CLI_VERSION}"
	;;
"api -H Accept: application/vnd.github.raw repos/devantler-tech/homebrew-tap/contents/Casks/ksail-desktop.rb")
	[[ "${FAKE_DESKTOP_VERSION}" == __error__ ]] && exit 1
	cask_body "${FAKE_DESKTOP_VERSION}"
	;;
"api repos/devantler-tech/ksail/releases/latest --jq .tag_name")
	[[ "${FAKE_LATEST_TAG}" == __error__ ]] && exit 1
	printf '%s\n' "${FAKE_LATEST_TAG}"
	;;
*)
	printf 'unexpected gh call: %s\n' "${command_line}" >&2
	exit 2
	;;
esac
EOF
chmod +x "${fake_bin}/gh"

pass_count=0

# run_case NAME TAG CLI_VERSION DESKTOP_VERSION LATEST_TAG EXPECTED_STATUS EXPECTED_PUBLISH EXPECTED_MESSAGE
# EXPECTED_PUBLISH is true, false, or none (no output line and nothing written to GITHUB_OUTPUT).
run_case() {
	local name="$1" tag="$2" cli="$3" desktop="$4" latest="$5"
	local expected_status="$6" expected_publish="$7" expected_message="$8"
	local case_dir="${tmp_dir}/${name}" output status stdout
	mkdir -p "${case_dir}"
	: >"${case_dir}/github-output"
	set +e
	output="$(PATH="${fake_bin}:${PATH}" \
		GITHUB_OUTPUT="${case_dir}/github-output" \
		FAKE_CLI_VERSION="${cli}" \
		FAKE_DESKTOP_VERSION="${desktop}" \
		FAKE_LATEST_TAG="${latest}" \
		"${resolver}" --tap devantler-tech/homebrew-tap --source-repo devantler-tech/ksail \
		--tag "${tag}" ksail ksail-desktop 2>"${case_dir}/stderr")"
	status=$?
	set -e
	stdout="${output}"
	output="${stdout}$(cat "${case_dir}/stderr")"

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' "${name}" "${expected_status}" "${status}" "${output}" >&2
		return 1
	fi
	if [[ "${expected_publish}" == none ]]; then
		if [[ -n "${stdout}" || -s "${case_dir}/github-output" ]]; then
			printf 'FAIL: %s: expected no decision, got stdout %q and output file:\n%s\n' \
				"${name}" "${stdout}" "$(cat "${case_dir}/github-output")" >&2
			return 1
		fi
	else
		if [[ "${stdout}" != "publish=${expected_publish}" ]] ||
			[[ "$(cat "${case_dir}/github-output")" != "publish=${expected_publish}" ]]; then
			printf 'FAIL: %s: expected publish=%s on stdout and in GITHUB_OUTPUT, got stdout %q and:\n%s\n' \
				"${name}" "${expected_publish}" "${stdout}" "$(cat "${case_dir}/github-output")" >&2
			return 1
		fi
	fi
	if [[ "${output}" != *"${expected_message}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' "${name}" "${expected_message}" "${output}" >&2
		return 1
	fi
	pass_count=$((pass_count + 1))
}

# Forward and equal releases may update the casks.
run_case newer v7.205.0 7.204.9 7.204.9 v7.204.9 0 true 'may be updated'
run_case equal v7.204.9 7.204.9 7.204.9 v7.204.9 0 true 'may be updated'
run_case numeric-not-lexical v7.204.10 7.204.9 7.204.9 v7.204.9 0 true 'may be updated'
run_case major-bump v8.0.0 7.204.9 7.204.9 v7.204.9 0 true 'may be updated'
# The 2026-10-20 incident: v7.204.1 retried after v7.204.4 was published and on both casks.
run_case older-after-newer v7.204.1 7.204.4 7.204.4 v7.204.4 0 false 'older than the cask ksail (7.204.4)'
run_case older-than-desktop-only v7.204.5 7.204.4 7.204.6 v7.204.4 0 false 'older than the cask ksail-desktop (7.204.6)'
# A cask delivery that lagged behind a newer release must not let an older retry move it.
run_case older-than-latest-release v7.204.3 7.204.1 7.204.1 v7.204.4 0 false 'older than the latest release (7.204.4)'
run_case prerelease v7.205.0-rc.1 7.204.9 7.204.9 v7.204.9 0 false 'not a stable version'
# Missing or unreadable evidence fails closed with no decision at all.
run_case cli-unreadable v7.205.0 __error__ 7.204.9 v7.204.9 1 none 'could not read Casks/ksail.rb'
run_case desktop-unreadable v7.205.0 7.204.9 __error__ v7.204.9 1 none 'could not read Casks/ksail-desktop.rb'
run_case version-missing v7.205.0 __missing__ 7.204.9 v7.204.9 1 none 'does not declare exactly one version'
run_case version-twice v7.205.0 __twice__ 7.204.9 v7.204.9 1 none 'does not declare exactly one version'
run_case cask-not-stable v7.205.0 7.204.9-rc.1 7.204.9 v7.204.9 1 none 'non-stable version'
run_case latest-unreadable v7.205.0 7.204.9 7.204.9 __error__ 1 none 'could not read the latest published release'
run_case latest-malformed v7.205.0 7.204.9 7.204.9 'not-a-tag' 1 none 'non-stable tag'

printf 'All %d cask publication order cases passed.\n' "${pass_count}"
