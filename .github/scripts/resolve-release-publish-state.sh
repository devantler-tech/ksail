#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  resolve-release-publish-state.sh --tag TAG [--repo OWNER/REPO] [--assets-dir DIR]

Decide what the publish job must do for TAG and print `state=draft` or
`state=published` (also appended to $GITHUB_OUTPUT when set).

  draft      exactly one unpublished release exists: attach assets, then publish.
  published  exactly one published release exists and it is complete: nothing is
             left to do, so a rerun succeeds without touching the immutable release.

Fails closed when no release exists, when more than one release carries the tag,
or when the published release is missing the installer assets or any asset from
--assets-dir (or carries one of them empty). --repo defaults to $GH_REPO, then
$GITHUB_REPOSITORY.
EOF
}

tag=""
repo="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
assets_dir=""

while (($# > 0)); do
	case "$1" in
	--tag)
		tag="${2:-}"
		shift 2
		;;
	--repo)
		repo="${2:-}"
		shift 2
		;;
	--assets-dir)
		assets_dir="${2:-}"
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

if [[ -z "${tag}" || -z "${repo}" ]]; then
	printf 'ERROR: --tag and a repository (--repo, GH_REPO or GITHUB_REPOSITORY) are required\n' >&2
	usage >&2
	exit 2
fi
if [[ -n "${assets_dir}" && ! -d "${assets_dir}" ]]; then
	printf 'ERROR: assets directory does not exist: %s\n' "${assets_dir}" >&2
	exit 2
fi

blocked() {
	printf 'BLOCKED: %s\n' "$1" >&2
	exit 1
}

# listReleases is the only listing that includes drafts. Read every page: the release for this
# tag is not guaranteed to be on the first one.
if ! pages="$(gh api --paginate "repos/${repo}/releases?per_page=100")"; then
	blocked "could not list releases for ${repo}"
fi
if ! matching="$(jq -s --arg tag "${tag}" '
	[.[] | if type == "array" then .[] else error("unexpected release page") end
	 | select(.tag_name == $tag)
	 | {id, draft, assets: [(.assets // [])[] | {name, size}]}]
' <<<"${pages}")"; then
	blocked "could not parse the release listing for ${repo}"
fi

count="$(jq 'length' <<<"${matching}")"
if [[ "${count}" -eq 0 ]]; then
	blocked "no release exists for tag ${tag}"
fi
if [[ "${count}" -gt 1 ]]; then
	blocked "found ${count} releases for tag ${tag} ($(jq -r 'map("\(.id) (draft=\(.draft))") | join(", ")' <<<"${matching}")); expected exactly one — manual cleanup required"
fi

state=""
if [[ "$(jq -r '.[0].draft' <<<"${matching}")" == "true" ]]; then
	state="draft"
else
	# A published release is immutable, so it can only be accepted as-is when nothing is missing:
	# the installer contract plus every asset this run would otherwise have attached.
	version="${tag#v}"
	required=("install.sh" "ksail_${version}_checksums.txt")
	if [[ -n "${assets_dir}" ]]; then
		while IFS= read -r -d '' asset; do
			required+=("$(basename -- "${asset}")")
		done < <(find "${assets_dir}" -maxdepth 1 -type f -print0)
	fi

	missing=()
	for name in "${required[@]}"; do
		if ! jq -e --arg name "${name}" '.[0].assets | any(.name == $name and .size > 0)' \
			<<<"${matching}" >/dev/null; then
			missing+=("${name}")
		fi
	done
	if ((${#missing[@]} > 0)); then
		blocked "published release ${tag} is incomplete; missing or empty assets: ${missing[*]}"
	fi
	state="published"
fi

printf 'state=%s\n' "${state}"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	printf 'state=%s\n' "${state}" >>"${GITHUB_OUTPUT}"
fi
