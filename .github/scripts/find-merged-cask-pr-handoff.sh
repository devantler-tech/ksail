#!/usr/bin/env bash

set -euo pipefail

# Describe the required release identity and optional known-PR reconciliation lead.
usage() {
	cat <<'EOF'
Usage:
  find-merged-cask-pr-handoff.sh --tap OWNER/REPO --cask-name NAME
                                 --source-repo OWNER/REPO --tag TAG
                                 --output-dir DIR [--pr NUMBER]

Find the newest fully validated merged PR for an evergreen GoReleaser cask
branch. Titles are not trusted: a later release can coalesce into the older-
titled open PR. Every candidate is refetched and validated against its immutable
merge result, a pinned current-main cask blob, release digests, and merge ancestry.
Print the validated PR number. Fail closed when enumeration or every candidate
validation fails.
With --pr, validate the already-selected PR directly before discovery and once
more after unsuccessful discovery. The number is only a lead; all merged
evidence gates still apply.
EOF
}

tap=""
cask_name=""
source_repo=""
tag=""
output_dir=""
known_pr=""

while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
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
	--pr)
		known_pr="${2:-}"
		if [[ ! "${known_pr}" =~ ^[1-9][0-9]*$ ]]; then
			printf 'ERROR: --pr requires a positive PR number\n' >&2
			exit 2
		fi
		shift 2
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

# Recollect a candidate and require immutable merge, current-main and release-digest proof.
# Preserve successful evidence while keeping candidate diagnostics off the PR-number output.
validate_candidate() {
	local pr="$1" evidence="${output_dir}/merged-$1.json"
	local candidate_log="${work}/candidate-$1.log"
	{
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
				--source-repo "${source_repo}" \
				--tag "${tag}" \
				--merged
	} >"${candidate_log}" 2>&1
}

if [[ -n "${known_pr}" ]]; then
	if validate_candidate "${known_pr}"; then
		printf '%s\n' "${known_pr}"
		exit 0
	fi
	printf '%s\n' "${known_pr}" >>"${work}/rejected"
fi

if ! gh api --paginate --slurp \
	"repos/${tap}/pulls?state=closed&head=${tap%%/*}:${branch}&per_page=100" \
	>"${work}/pages.json" 2>"${work}/discovery.log"; then
	if [[ -n "${known_pr}" ]] && validate_candidate "${known_pr}"; then
		printf '%s\n' "${known_pr}"
		exit 0
	fi
	printf 'BLOCKED: could not enumerate merged cask PRs for %s\n' "${branch}" >&2
	exit 1
fi
if ! jq -e 'type == "array" and all(.[]; type == "array")' \
	"${work}/pages.json" >/dev/null 2>&1; then
	if [[ -n "${known_pr}" ]] && validate_candidate "${known_pr}"; then
		printf '%s\n' "${known_pr}"
		exit 0
	fi
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

if [[ ! -s "${work}/candidates" && -z "${known_pr}" ]]; then
	printf 'BLOCKED: no trusted merged cask PR found for %s\n' "${branch}" >&2
	exit 1
fi

while IFS= read -r pr; do
	[[ "${pr}" =~ ^[1-9][0-9]*$ ]] || continue
	[[ "${pr}" == "${known_pr}" ]] && continue
	if validate_candidate "${pr}"; then
		printf '%s\n' "${pr}"
		exit 0
	fi
	printf '%s\n' "${pr}" >>"${work}/rejected"
done <"${work}/candidates"

# Historical validation can take long enough for the selected PR to merge after
# the list snapshot. Rebind that known identity once before reporting failure.
if [[ -n "${known_pr}" ]] && validate_candidate "${known_pr}"; then
	printf '%s\n' "${known_pr}"
	exit 0
fi

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
