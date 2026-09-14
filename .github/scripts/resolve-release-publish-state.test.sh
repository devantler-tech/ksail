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
crd_dir="${tmp_dir}/crds"
crd_glob="${crd_dir}/*.yaml"
pass_count=0

mkdir -p "${fake_bin}" "${assets_dir}" "${crd_dir}"
printf 'vsix\n' >"${assets_dir}/ksail-7.175.1.vsix"
printf 'zip\n' >"${assets_dir}/KSail_7.175.1_darwin_arm64.zip"
printf 'kind: CustomResourceDefinition\n' >"${crd_dir}/ksail.io_clusters.yaml"

# The fake serves the release listing for FAKE_GH_SCENARIO (one JSON array per page, as
# `gh api --paginate` prints) and the release's checksums file for FAKE_GH_CHECKSUMS.
cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
listing='api --paginate repos/devantler-tech/ksail/releases?per_page=100'
download='release download v7.175.1 --repo devantler-tech/ksail --pattern ksail_7.175.1_checksums.txt --output -'
if [[ "$*" == "${download}" ]]; then
	case "${FAKE_GH_CHECKSUMS}" in
	complete)
		printf '%s  ksail_7.175.1_darwin_arm64.tar.gz\n' "$(printf 'a%.0s' {1..64})"
		printf '%s  ksail_7.175.1_linux_amd64.tar.gz\n' "$(printf 'b%.0s' {1..64})"
		printf '%s  ksail_7.175.1_windows_amd64.zip\n' "$(printf 'c%.0s' {1..64})"
		;;
	empty) ;;
	malformed) printf 'not a checksum line\n' ;;
	fail)
		printf 'HTTP 404\n' >&2
		exit 1
		;;
	*)
		printf 'unknown checksums scenario\n' >&2
		exit 3
		;;
	esac
	exit 0
fi
[[ "$*" == "${listing}" ]] || {
	printf 'unexpected gh call: %s\n' "$*" >&2
	exit 3
}
complete='[
  {"name":"install.sh","size":10},
  {"name":"ksail_7.175.1_checksums.txt","size":10},
  {"name":"ksail-7.175.1.vsix","size":10},
  {"name":"KSail_7.175.1_darwin_arm64.zip","size":10},
  {"name":"ksail.io_clusters.yaml","size":10},
  {"name":"ksail_7.175.1_darwin_arm64.tar.gz","size":10},
  {"name":"ksail_7.175.1_linux_amd64.tar.gz","size":10},
  {"name":"ksail_7.175.1_windows_amd64.zip","size":10}
]'
# without prints the complete asset list minus the named asset.
without() { jq -c --arg name "$1" 'map(select(.name != $name))' <<<"${complete}"; }
# emptied prints the complete asset list with the named asset's size set to zero.
emptied() { jq -c --arg name "$1" 'map(if .name == $name then .size = 0 else . end)' <<<"${complete}"; }
# published prints one listing page holding the published release with the given assets.
published() { printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":%s}]\n' "$1"; }
other='{"id":1,"tag_name":"v7.175.0","draft":false,"assets":[]}'
case "${FAKE_GH_SCENARIO}" in
draft) printf '[%s,{"id":2,"tag_name":"v7.175.1","draft":true,"assets":[]}]\n' "${other}" ;;
published) published "${complete}" ;;
second-page) printf '[%s]\n' "${other}" && published "${complete}" ;;
draft-second-page) printf '[%s]\n[{"id":2,"tag_name":"v7.175.1","draft":true,"assets":[]}]\n' "${other}" ;;
incomplete-second-page) printf '[%s]\n' "${other}" && published "$(without ksail_7.175.1_linux_amd64.tar.gz)" ;;
missing) printf '[%s]\n' "${other}" ;;
duplicate)
	printf '[{"id":2,"tag_name":"v7.175.1","draft":false,"assets":%s},{"id":3,"tag_name":"v7.175.1","draft":true,"assets":[]}]\n' "${complete}"
	;;
published-without-checksums) published "$(without ksail_7.175.1_checksums.txt)" ;;
published-without-artifact) published "$(without KSail_7.175.1_darwin_arm64.zip)" ;;
published-empty-checksums) published "$(emptied ksail_7.175.1_checksums.txt)" ;;
published-without-crd) published "$(without ksail.io_clusters.yaml)" ;;
published-without-archive) published "$(without ksail_7.175.1_linux_amd64.tar.gz)" ;;
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

# run_case runs the resolver against one fake release listing and checks its exit status, output and
# GITHUB_OUTPUT. Arguments: name scenario expected_status expected_output [checksums] [extra_glob].
# Set resolver_under_test to run a modified copy of the resolver instead of the real one.
run_case() {
	local name="$1" scenario="$2" expected_status="$3" expected_output="$4"
	local checksums="${5:-complete}" glob="${6:-${crd_glob}}"
	local output status github_output="${tmp_dir}/${name}.output"

	: >"${github_output}"
	set +e
	output="$(PATH="${fake_bin}:${PATH}" FAKE_GH_SCENARIO="${scenario}" FAKE_GH_CHECKSUMS="${checksums}" \
		GITHUB_OUTPUT="${github_output}" GH_REPO=devantler-tech/ksail \
		"${resolver_under_test:-${resolver}}" --tag v7.175.1 --assets-dir "${assets_dir}" --extra-assets-glob "${glob}" 2>&1)"
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
# A draft still gets every asset attached, so its checksums are never read.
run_case draft-release-reads-no-checksums draft 0 'state=draft' fail
run_case published-release published 0 'state=published'
run_case published-release-on-a-later-page second-page 0 'state=published'
run_case missing-release missing 1 'no release exists for tag v7.175.1'
run_case duplicate-releases duplicate 1 'found 2 releases for tag v7.175.1'
run_case published-without-checksums-asset published-without-checksums 1 'missing or empty assets: ksail_7.175.1_checksums.txt'
run_case published-without-downloaded-artifact published-without-artifact 1 'missing or empty assets: KSail_7.175.1_darwin_arm64.zip'
run_case published-with-empty-asset published-empty-checksums 1 'missing or empty assets: ksail_7.175.1_checksums.txt'
run_case published-without-crd published-without-crd 1 'missing or empty assets: ksail.io_clusters.yaml'
run_case published-without-goreleaser-archive published-without-archive 1 'missing or empty assets: ksail_7.175.1_linux_amd64.tar.gz'
run_case published-checksums-unreadable published 1 'could not read ksail_7.175.1_checksums.txt' fail
run_case published-checksums-empty published 1 'lists no assets' empty
run_case published-checksums-malformed published 1 'has a malformed line' malformed
run_case extra-assets-glob-matches-nothing published 1 'extra assets glob matched no files' complete "${tmp_dir}/absent/*.yaml"
run_case release-listing-failure list-failure 1 'could not list releases'
run_case draft-release-on-a-later-page draft-second-page 0 'state=draft'
run_case incomplete-published-release-on-a-later-page incomplete-second-page 1 'missing or empty assets: ksail_7.175.1_linux_amd64.tar.gz'

# The published-state guard is what turns a rerun into a no-op. With it removed, the same complete
# published release resolves as a draft, so the workflow would re-attach assets and republish the
# immutable release: the failure #6263 describes.
mutant="${tmp_dir}/resolve-release-publish-state.mutant.sh"
if ! awk 'index($0, ".[0].draft") && index($0, "== \"true\"") { print "if true; then"; mutated++; next }
	{ print }
	END { exit mutated == 1 ? 0 : 1 }' "${resolver}" >"${mutant}"; then
	printf 'FAIL: the published-state guard was not found exactly once in the resolver\n' >&2
	exit 1
fi
chmod +x "${mutant}"
resolver_under_test="${mutant}" run_case published-guard-removed-republishes published 0 'state=draft'

# The workflow must resolve the state BEFORE touching the release and gate both mutations on a draft:
# the upload is what failed on a rerun against the already-published v7.175.1.
publish_block="${tmp_dir}/publish-release.yaml"
awk '/^  publish-release:/ { inside = 1; print; next } inside && /^  [a-z]/ { exit } inside { print }' \
	"${cd_workflow}" >"${publish_block}"
[[ -s "${publish_block}" ]] || {
	printf 'FAIL: publish-release job not found in cd.yaml\n' >&2
	exit 1
}
# line_of prints the first line number of the given text in the publish-release job, or nothing when
# the text is absent, so the ordering checks below report a missing step instead of passing.
line_of() {
	grep -nF -- "$1" "${publish_block}" | head -n 1 | cut -d: -f1 || true
}
checkout_line="$(line_of 'uses: actions/checkout@')"
download_line="$(line_of 'name: 📥 Download release asset artifacts')"
resolve_line="$(line_of 'resolve-release-publish-state.sh --tag')"
attach_line="$(line_of 'name: 📤 Attach assets to draft release')"
publish_line="$(line_of 'name: 📢 Publish draft release')"
# A checkout into a fresh workspace clears it, so it must run before the assets are downloaded.
if [[ -z "${checkout_line}" || -z "${download_line}" || -z "${resolve_line}" ]] ||
	((checkout_line >= download_line || download_line >= resolve_line)); then
	printf 'FAIL: publish-release must check out before downloading the release assets it resolves\n' >&2
	exit 1
fi
if [[ -z "${attach_line}" || -z "${publish_line}" ]] ||
	((resolve_line >= attach_line || attach_line >= publish_line)); then
	printf 'FAIL: publish-release must resolve the release state before attaching and publishing\n' >&2
	exit 1
fi
gated="$(grep -cF -- "if: steps.release_state.outputs.state == 'draft'" "${publish_block}" || true)"
if [[ "${gated}" -ne 2 ]]; then
	printf 'FAIL: both the attach and publish steps must run only for a draft release (found %s gates)\n' "${gated}" >&2
	exit 1
fi
# GoReleaser attaches the Cluster CRD itself, so the resolver must be told to require it.
if ! grep -qF -- "--extra-assets-glob 'charts/ksail-operator/crds/*.yaml'" "${publish_block}"; then
	printf 'FAIL: publish-release must require the attached Cluster CRD when accepting a published release\n' >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: workflow-gates-release-mutations\n'

# The resolver reads every page, so both workflow release lookups must too: otherwise a draft beyond the
# first 100 releases reaches publication and is not found, and cleanup misses a published release.
single_page="$(grep -cF -- 'await github.rest.repos.listReleases(' "${cd_workflow}" || true)"
paginated="$(grep -cF -- 'await github.paginate(github.rest.repos.listReleases,' "${cd_workflow}" || true)"
if [[ "${single_page}" -ne 0 || "${paginated}" -lt 2 ]]; then
	printf 'FAIL: cd.yaml release lookups must read every page (single-page=%s paginated=%s)\n' "${single_page}" "${paginated}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: workflow-release-lookups-paginate\n'

# CI must execute this suite, not only lint it: path filter, shellcheck, and the run line.
ci_workflow="${repo_root}/.github/workflows/ci.yaml"
executed="$(grep -cxE '[[:space:]]*\.github/scripts/resolve-release-publish-state\.test\.sh' "${ci_workflow}" || true)"
if [[ "${executed}" -lt 1 ]]; then
	printf 'FAIL: ci.yaml must execute .github/scripts/resolve-release-publish-state.test.sh\n' >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: ci-executes-release-publish-state-suite\n'

printf 'All %d release publish-state cases passed.\n' "${pass_count}"
