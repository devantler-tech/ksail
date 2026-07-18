#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  find-merged-cask-pr-handoff.sh --tap OWNER/REPO --cask-name NAME
                                 --source-repo OWNER/REPO --tag TAG
                                 --output-dir DIR

Find the newest fully validated merged PR for an evergreen GoReleaser cask
branch. Titles are not trusted: a later release can coalesce into the older-
titled open PR. Every candidate is refetched and validated against its immutable
merge result, a pinned current-main cask blob, release digests, and merge ancestry.
Print the validated PR number. Fail closed when enumeration or every candidate
validation fails.
EOF
}

tap=""
cask_name=""
source_repo=""
tag=""
output_dir=""

while (($# > 0)); do
	case "$1" in
	--tap)
		tap="${2:-}"
		shift 2
		;;
	--cask-name)
		cask_name="${2:-}"
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
	--output-dir)
		output_dir="${2:-}"
		shift 2
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

if [[ ! "${tap}" =~ ^[^/]+/[^/]+$ || -z "${cask_name}" ||
	! "${source_repo}" =~ ^[^/]+/[^/]+$ || -z "${tag}" ||
	! -d "${output_dir}" ]]; then
	printf 'ERROR: --tap OWNER/REPO, --cask-name NAME, --source-repo OWNER/REPO, --tag TAG, and an existing --output-dir DIR are required\n' >&2
	exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
branch="goreleaser/${cask_name}"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if ! gh api --paginate --slurp \
	"repos/${tap}/pulls?state=closed&head=${tap%%/*}:${branch}&per_page=100" \
	>"${work}/pages.json"; then
	printf 'BLOCKED: could not enumerate merged cask PRs for %s\n' "${branch}" >&2
	exit 1
fi
if ! jq -e 'type == "array" and all(.[]; type == "array")' \
	"${work}/pages.json" >/dev/null 2>&1; then
	printf 'BLOCKED: could not enumerate merged cask PRs for %s: malformed response\n' \
		"${branch}" >&2
	exit 1
fi

# The list response is discovery only. Filter obvious mismatches, deduplicate across pages, then
# refetch every candidate through the collector before trusting any field or content evidence.
jq -r \
	--arg full "${tap}" \
	--arg branch "${branch}" '
    [
      .[][]
      | select(
          (.number | type == "number")
          and (.merged_at | type == "string" and length > 0)
          and .user.login == "devantler"
          and .head.repo.full_name == $full
          and .head.ref == $branch
          and .base.repo.full_name == $full
          and .base.ref == "main"
        )
    ]
    | unique_by(.number)
    | sort_by(.merged_at)
    | reverse
    | .[].number
  ' "${work}/pages.json" >"${work}/candidates"

if [[ ! -s "${work}/candidates" ]]; then
	printf 'BLOCKED: no trusted merged cask PR found for %s\n' "${branch}" >&2
	exit 1
fi

while IFS= read -r pr; do
	[[ "${pr}" =~ ^[1-9][0-9]*$ ]] || continue
	evidence="${output_dir}/merged-${pr}.json"
	candidate_log="${work}/candidate-${pr}.log"
	if {
		"${script_dir}/collect-cask-pr-handoff.sh" \
			--tap "${tap}" \
			--pr "${pr}" \
			--source-repo "${source_repo}" \
			--tag "${tag}" \
			--output "${evidence}" \
			--include-main &&
			"${script_dir}/validate-cask-pr-handoff.sh" \
				--evidence "${evidence}" \
				--tap "${tap}" \
				--cask-name "${cask_name}" \
				--tag "${tag}" \
				--merged
	} >"${candidate_log}" 2>&1; then
		printf '%s\n' "${pr}"
		exit 0
	fi
	printf '%s\n' "${pr}" >>"${work}/rejected"
done <"${work}/candidates"

printf 'BLOCKED: no valid merged cask PR found for %s and %s\n' "${branch}" "${tag}" >&2
if [[ -s "${work}/rejected" ]]; then
	while IFS= read -r pr; do
		candidate_log="${work}/candidate-${pr}.log"
		reason="$(<"${candidate_log}")"
		reason="${reason//$'\n'/; }"
		printf -- '- PR #%s: %s\n' "${pr}" "${reason:-validation failed without diagnostics}" >&2
	done <"${work}/rejected"
fi
exit 1
