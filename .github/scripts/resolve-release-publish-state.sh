#!/usr/bin/env bash

set -euo pipefail

# usage prints the command-line contract, including what makes a published release complete.
usage() {
	cat <<'EOF'
Usage:
  resolve-release-publish-state.sh --tag TAG [--repo OWNER/REPO] [--assets-dir DIR]
                                   [--extra-assets-glob GLOB]...

Decide what the publish job must do for TAG and print `state=draft` or
`state=published` (also appended to $GITHUB_OUTPUT when set).

  draft      exactly one unpublished release exists and it carries everything
             GoReleaser uploads: attach assets, then publish.
  published  exactly one published release exists and it is complete: nothing is
             left to do, so a rerun succeeds without touching the immutable release.

Either release must carry, non-empty:
  - ksail_<version>_checksums.txt,
  - install.sh (the installer GoReleaser uploads as an extra file),
  - the basename of every file matched by each --extra-assets-glob (for example
    the Cluster CRD GoReleaser attaches), and
  - every archive listed in the release's own checksums file (the archives
    GoReleaser uploads directly), each with a recorded SHA-256 digest equal to
    its listed checksum.
A published release must also carry every file in --assets-dir (the artifacts
this job would attach).

Fails closed when no release exists, when more than one release carries the tag,
when the release is incomplete, when its checksums file cannot be read or lists
nothing, when an archive's digest is missing or differs from its checksum, or when
an extra-assets glob matches no file. --repo defaults to $GH_REPO, then
$GITHUB_REPOSITORY.
EOF
}

tag=""
repo="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
assets_dir=""
extra_globs=()

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
	--extra-assets-glob)
		extra_globs+=("${2:-}")
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

# blocked reports why the publish job cannot proceed and exits non-zero without writing a state.
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
	 | {id, draft, assets: [(.assets // [])[] | {id, name, size, digest}]}]
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

# missing_assets prints the names from its arguments that the release lacks or carries empty.
missing_assets() {
	local name
	for name in "$@"; do
		if ! jq -e --arg name "${name}" '.[0].assets | any(.name == $name and .size > 0)' \
			<<<"${matching}" >/dev/null; then
			printf '%s\n' "${name}"
		fi
	done
}

# require_assets blocks when the release lacks, or carries empty, any of the named assets.
require_assets() {
	local missing=() name
	while IFS= read -r name; do
		missing+=("${name}")
	done < <(missing_assets "$@")
	if ((${#missing[@]} > 0)); then
		blocked "${kind} ${tag} is incomplete; missing or empty assets: ${missing[*]}"
	fi
}

if [[ "$(jq -r '.[0].draft' <<<"${matching}")" == "true" ]]; then
	state="draft"
	kind="draft release"
else
	state="published"
	kind="published release"
fi

# GoReleaser uploads its checksums file, the extra assets and its archives straight to the release,
# and the publish job attaches only --assets-dir. So a draft must already carry everything GoReleaser
# produced; a published release is immutable, so it must also carry what the publish job attaches.
version="${tag#v}"
checksums_name="ksail_${version}_checksums.txt"
# install.sh is a GoReleaser extra file, so a draft must already carry it: checking it only once the
# release is published would let an incomplete draft reach the upload and publish steps.
required=("${checksums_name}" "install.sh")
for glob in "${extra_globs[@]+"${extra_globs[@]}"}"; do
	matched=false
	while IFS= read -r extra; do
		[[ -n "${extra}" ]] || continue
		required+=("$(basename -- "${extra}")")
		matched=true
	done < <(compgen -G "${glob}" || true)
	if [[ "${matched}" != true ]]; then
		blocked "extra assets glob matched no files: ${glob}"
	fi
done
if [[ "${state}" == "published" ]]; then
	if [[ -n "${assets_dir}" ]]; then
		while IFS= read -r -d '' asset; do
			required+=("$(basename -- "${asset}")")
		done < <(find "${assets_dir}" -maxdepth 1 -type f -print0)
	fi
fi
require_assets "${required[@]}"

# The checksums file names every GoReleaser archive. Read it by asset id: tag-based downloads cannot
# see a draft.
checksums_id="$(jq -r --arg name "${checksums_name}" '[.[0].assets[] | select(.name == $name)][0].id' <<<"${matching}")"
if ! checksums="$(gh api "repos/${repo}/releases/assets/${checksums_id}" -H 'Accept: application/octet-stream')"; then
	blocked "could not read ${checksums_name} from ${kind} ${tag}"
fi
listed=()
sums=()
while IFS= read -r line; do
	[[ -n "${line}" ]] || continue
	if [[ "${line}" =~ ^([[:xdigit:]]{64})[[:space:]]+\*?([^[:space:]/]+)$ ]]; then
		sums+=("${BASH_REMATCH[1]}")
		listed+=("${BASH_REMATCH[2]}")
	else
		blocked "${checksums_name} for ${kind} ${tag} has a malformed line"
	fi
done <<<"${checksums}"
if ((${#listed[@]} == 0)); then
	blocked "${checksums_name} for ${kind} ${tag} lists no assets"
fi
require_assets "${listed[@]}"

# GitHub records a SHA-256 digest for every uploaded asset, so each listed archive is verified against
# its checksum without downloading it. An asset without a recorded digest cannot be verified.
for i in "${!listed[@]}"; do
	if ! jq -e --arg name "${listed[i]}" --arg sum "${sums[i]}" \
		'.[0].assets | any(.name == $name and .digest == ("sha256:" + ($sum | ascii_downcase)))' \
		<<<"${matching}" >/dev/null; then
		blocked "${kind} ${tag}: ${listed[i]} does not match its checksum in ${checksums_name}"
	fi
done

printf 'state=%s\n' "${state}"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	printf 'state=%s\n' "${state}" >>"${GITHUB_OUTPUT}"
fi
