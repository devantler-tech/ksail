#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  collect-cask-pr-handoff.sh --tap OWNER/REPO --pr NUMBER
                             --source-repo OWNER/REPO --tag TAG --output FILE
                             [--include-main]

Collect a stable normalized snapshot of a generated Homebrew cask pull request:
REST PR identity, every paginated changed file, the repository label inventory,
the source release's asset digests for TAG, and — when exactly one file changed —
that file's content at the PR head, so the validator can prove the cask actually
pins the release (and the exact artifacts) being handed off.
With --include-main, require a closed merged PR and also capture the cask at its
immutable merge commit, the cask at a pinned tap-main SHA, and ancestry evidence.
The tap-main ref is read again before publishing the snapshot; movement fails
collection instead of mixing evidence from two main revisions.
Any API or response-shape failure exits non-zero without publishing partial output.
EOF
}

tap=""
pr=""
source_repo=""
tag=""
output=""
include_main=false

while (($# > 0)); do
	case "$1" in
	--tap)
		tap="${2:-}"
		shift 2
		;;
	--pr)
		pr="${2:-}"
		shift 2
		;;
	--source-repo)
		source_repo="${2:-}"
		shift 2
		;;
	--tag)
		tag="${2:-}"
		shift 2
		;;
	--output)
		output="${2:-}"
		shift 2
		;;
	--include-main)
		include_main=true
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		printf 'ERROR: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "${tap}" =~ ^[^/]+/[^/]+$ || ! "${pr}" =~ ^[1-9][0-9]*$ ||
	! "${source_repo}" =~ ^[^/]+/[^/]+$ || -z "${tag}" || -z "${output}" ]]; then
	printf 'ERROR: --tap OWNER/REPO, --pr NUMBER, --source-repo OWNER/REPO, --tag TAG, and --output FILE are required\n' >&2
	usage >&2
	exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

gh api "repos/${tap}/pulls/${pr}" >"${work}/pr.json"
gh api --paginate --slurp \
	"repos/${tap}/pulls/${pr}/files?per_page=100" >"${work}/file-pages.json"
gh api --paginate --slurp \
	"repos/${tap}/labels?per_page=100" >"${work}/label-pages.json"
gh api "repos/${source_repo}/releases/tags/${tag}" >"${work}/release.json"

jq -e 'type == "object"' "${work}/pr.json" >/dev/null
jq -e 'type == "array" and all(.[]; type == "array")' \
	"${work}/file-pages.json" >/dev/null
jq -e '
  type == "array"
  and all(.[]; type == "array")
  and all(.[][]; (.name | type == "string" and length > 0))
' \
	"${work}/label-pages.json" >/dev/null
# The evergreen cask branch is reused across releases (and across re-runs of a failed
# release), so the validator must be able to prove the cask's sha256 stanzas point at
# THIS release's published artifacts — capture each asset's GitHub-computed digest.
jq -e '
  .assets
  | type == "array"
  and all(.[]; (.name | type == "string" and length > 0))
' "${work}/release.json" >/dev/null
jq '[.assets[] | {name, digest}]' "${work}/release.json" >"${work}/release-assets.json"

# When the PR changes exactly one file (the only shape the validator accepts),
# capture that file's content at the PR head so the validator can check the cask
# pins the current release. Any other shape records null — the validator blocks
# on file scope and on missing content evidence alike (fail closed).
printf 'null\n' >"${work}/head-file.json"
head_sha="$(jq -r '.head.sha // empty' "${work}/pr.json")"
single_file="$(jq -r 'if ([.[][]] | length) == 1 then [.[][]][0].filename else "" end' \
	"${work}/file-pages.json")"
if [[ -n "${head_sha}" && -n "${single_file}" ]]; then
	gh api "repos/${tap}/contents/${single_file}?ref=${head_sha}" >"${work}/head-file.json"
	jq -e '
    type == "object"
    and (.path | type == "string" and length > 0)
    and (.content | type == "string")
  ' "${work}/head-file.json" >/dev/null
fi

# Merged fallback evidence is deliberately opt-in so the normal open-PR handoff does not gain
# extra mutable-main API dependencies. For a coalesced release, bind the PR's one changed cask to
# both its immutable merge result and a pinned current-main snapshot, and prove the merge commit is
# in that main history. Re-read main before publishing so a concurrent tap merge cannot mix refs.
printf 'null\n' >"${work}/merge-file.json"
printf 'null\n' >"${work}/main-ref.json"
printf 'null\n' >"${work}/main-file.json"
printf 'null\n' >"${work}/merge-comparison.json"
if [[ "${include_main}" == true ]]; then
	jq -e '
    .state == "closed"
    and .merged == true
    and (.merged_at | type == "string" and length > 0)
    and (.merge_commit_sha | type == "string" and test("^[0-9a-fA-F]{40}$"))
  ' "${work}/pr.json" >/dev/null
	if [[ -z "${single_file}" ]]; then
		printf 'ERROR: --include-main requires exactly one changed PR file\n' >&2
		exit 1
	fi

	gh api "repos/${tap}/git/ref/heads/main" >"${work}/main-ref-raw.json"
	jq -e '
    .object.sha
    | type == "string" and test("^[0-9a-fA-F]{40}$")
  ' "${work}/main-ref-raw.json" >/dev/null
	merge_commit_sha="$(jq -r '.merge_commit_sha' "${work}/pr.json")"
	main_sha="$(jq -r '.object.sha' "${work}/main-ref-raw.json")"

	gh api "repos/${tap}/contents/${single_file}?ref=${merge_commit_sha}" \
		>"${work}/merge-file.json"
	gh api "repos/${tap}/contents/${single_file}?ref=${main_sha}" \
		>"${work}/main-file.json"
	gh api "repos/${tap}/compare/${merge_commit_sha}...${main_sha}" \
		>"${work}/merge-comparison.json"
	for content_file in merge-file main-file; do
		jq -e --arg path "${single_file}" '
      type == "object"
      and .path == $path
      and (.sha | type == "string" and test("^[0-9a-fA-F]{40}$"))
      and (.content | type == "string")
    ' "${work}/${content_file}.json" >/dev/null
	done
	jq -e '
    type == "object"
    and (.status | type == "string")
    and (.behind_by | type == "number")
    and (.merge_base_commit.sha | type == "string")
  ' "${work}/merge-comparison.json" >/dev/null

	gh api "repos/${tap}/git/ref/heads/main" >"${work}/main-ref-after.json"
	if [[ "$(jq -r '.object.sha // empty' "${work}/main-ref-after.json")" != "${main_sha}" ]]; then
		printf 'ERROR: tap main moved while merged handoff evidence was collected\n' >&2
		exit 1
	fi
	jq -n --arg sha "${main_sha}" '{sha: $sha}' >"${work}/main-ref.json"
fi

jq -n \
	--slurpfile pr "${work}/pr.json" \
	--slurpfile pages "${work}/file-pages.json" \
	--slurpfile label_pages "${work}/label-pages.json" \
	--slurpfile release_assets "${work}/release-assets.json" \
	--slurpfile head_file "${work}/head-file.json" \
	--slurpfile merge_file "${work}/merge-file.json" \
	--slurpfile main_ref "${work}/main-ref.json" \
	--slurpfile main_file "${work}/main-file.json" \
	--slurpfile merge_comparison "${work}/merge-comparison.json" '
    {
      pr: $pr[0],
      files: [$pages[0][][]],
      availableLabels: [$label_pages[0][][]],
      releaseAssets: $release_assets[0],
      headFile: $head_file[0],
      mergeFile: $merge_file[0],
      mainRef: $main_ref[0],
      mainFile: $main_file[0],
      mergeComparison: $merge_comparison[0]
    }
  ' >"${work}/evidence.json"

mv "${work}/evidence.json" "${output}"
