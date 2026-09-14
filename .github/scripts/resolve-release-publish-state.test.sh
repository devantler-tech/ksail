#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
resolver="${script_dir}/resolve-release-publish-state.sh"
cd_workflow="${repo_root}/.github/workflows/cd.yaml"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
assets_dir="${tmp_dir}/dist-assets"
pass_count=0

mkdir -p "${fake_bin}" "${assets_dir}"
printf 'vsix\n' >"${assets_dir}/ksail-7.175.1.vsix"
printf 'zip\n' >"${assets_dir}/KSail_7.175.1_darwin_arm64.zip"

# The fake prints the release listing for FAKE_GH_SCENARIO, one JSON array per page as
# `gh api --paginate` does.
cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "api --paginate repos/devantler-tech/ksail/releases?per_page=100" ]] || {
	printf 'unexpected gh call: %s\n' "$*" >&2
	exit 3
}
complete='[
  {"name":"install.sh","size":10},
  {"name":"ksail_7.175.1_checksums.txt","size":10},
  {"name":"ksail-7.175.1.vsix","size":10},
  {"name":"KSail_7.175.1_darwin_arm64.zip","size":10}
]'
other='{"id":1,"tag_name":"v7.175.0","draft":false,"assets":[]}'
case "${FAKE_GH_SCENARIO}" in
draft)
	printf '[%s,{"id":2,"tag_name":"v7.175.1","draft":true,"assets":[]}]\n' "${other}"
	;;
published)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":%s},%s]\n' "${complete}" "${other}"
	;;
second-page)
	printf '[%s]\n[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":%s}]\n' "${other}" "${complete}"
	;;
missing)
	printf '[%s]\n' "${other}"
	;;
duplicate)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":%s},{"id":3,"tag_name":"v7.175.1","draft":true,"assets":[]}]\n' "${complete}"
	;;
published-without-installer)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":[{"name":"install.sh","size":10},{"name":"ksail-7.175.1.vsix","size":10},{"name":"KSail_7.175.1_darwin_arm64.zip","size":10}]}]\n'
	;;
published-without-artifact)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":[{"name":"install.sh","size":10},{"name":"ksail_7.175.1_checksums.txt","size":10},{"name":"ksail-7.175.1.vsix","size":10}]}]\n'
	;;
published-empty-asset)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":[{"name":"install.sh","size":10},{"name":"ksail_7.175.1_checksums.txt","size":0},{"name":"ksail-7.175.1.vsix","size":10},{"name":"KSail_7.175.1_darwin_arm64.zip","size":10}]}]\n'
	;;
list-failure)
	printf 'HTTP 502\n' >&2
	exit 1
	;;
*)
	printf 'unknown scenario\n' >&2
	exit 3
	;;
esac
EOF
chmod +x "${fake_bin}/gh"

run_case() {
	local name="$1" scenario="$2" expected_status="$3" expected_output="$4"
	local output status github_output="${tmp_dir}/${name}.output"

	: >"${github_output}"
	set +e
	output="$(PATH="${fake_bin}:${PATH}" FAKE_GH_SCENARIO="${scenario}" GITHUB_OUTPUT="${github_output}" \
		GH_REPO=devantler-tech/ksail "${resolver}" --tag v7.175.1 --assets-dir "${assets_dir}" 2>&1)"
	status=$?
	set -e

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' "${name}" "${expected_status}" "${status}" "${output}" >&2
		exit 1
	fi
	if [[ "${output}" != *"${expected_output}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' "${name}" "${expected_output}" "${output}" >&2
		exit 1
	fi
	if [[ "${expected_status}" -eq 0 ]] && ! grep -Fxq -- "${expected_output}" "${github_output}"; then
		printf 'FAIL: %s: GITHUB_OUTPUT does not carry %s\n' "${name}" "${expected_output}" >&2
		exit 1
	fi
	if [[ "${expected_status}" -ne 0 && -s "${github_output}" ]]; then
		printf 'FAIL: %s: a blocked resolution must not write a state output\n' "${name}" >&2
		exit 1
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${name}"
}

run_case draft-release draft 0 'state=draft'
run_case published-release published 0 'state=published'
run_case published-release-on-a-later-page second-page 0 'state=published'
run_case missing-release missing 1 'no release exists for tag v7.175.1'
run_case duplicate-releases duplicate 1 'found 2 releases for tag v7.175.1'
run_case published-without-installer-asset published-without-installer 1 'missing or empty assets: ksail_7.175.1_checksums.txt'
run_case published-without-downloaded-artifact published-without-artifact 1 'missing or empty assets: KSail_7.175.1_darwin_arm64.zip'
run_case published-with-empty-asset published-empty-asset 1 'missing or empty assets: ksail_7.175.1_checksums.txt'
run_case release-listing-failure list-failure 1 'could not list releases'

# The workflow must resolve the state BEFORE touching the release and gate both mutations on a draft:
# the upload is what failed on a rerun against the already-published v7.175.1.
publish_block="${tmp_dir}/publish-release.yaml"
awk '/^  publish-release:/ { inside = 1; print; next } inside && /^  [a-z]/ { exit } inside { print }' \
	"${cd_workflow}" >"${publish_block}"
[[ -s "${publish_block}" ]] || {
	printf 'FAIL: publish-release job not found in cd.yaml\n' >&2
	exit 1
}
line_of() { # prints nothing when the text is absent, so the check below reports it
	grep -nF -- "$1" "${publish_block}" | head -n 1 | cut -d: -f1 || true
}
resolve_line="$(line_of 'resolve-release-publish-state.sh --tag')"
attach_line="$(line_of 'name: 📤 Attach assets to draft release')"
publish_line="$(line_of 'name: 📢 Publish draft release')"
if [[ -z "${resolve_line}" || -z "${attach_line}" || -z "${publish_line}" ]] ||
	((resolve_line >= attach_line || attach_line >= publish_line)); then
	printf 'FAIL: publish-release must resolve the release state before attaching and publishing\n' >&2
	exit 1
fi
gated="$(grep -cF -- "if: steps.release_state.outputs.state == 'draft'" "${publish_block}" || true)"
if [[ "${gated}" -ne 2 ]]; then
	printf 'FAIL: both the attach and publish steps must run only for a draft release (found %s gates)\n' "${gated}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: workflow-gates-release-mutations\n'

printf 'All %d release publish-state cases passed.\n' "${pass_count}"
