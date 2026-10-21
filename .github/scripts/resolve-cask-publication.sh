#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

usage() {
	cat <<'EOF'
Usage:
  resolve-cask-publication.sh --tap OWNER/REPO --source-repo OWNER/REPO --tag vX.Y.Z CASK_NAME [CASK_NAME...]

Decides whether this release may update the shared Homebrew cask pointers.

Prints exactly one line, `publish=true` or `publish=false`, and appends the same line to
$GITHUB_OUTPUT when it is set.

  publish=true   The tag is a stable version at least as new as every named cask on the tap's
                 default branch and the source repository's latest published release.
  publish=false  The tag is a prerelease, or an older stable version. The release still
                 publishes its own versioned assets; only the shared cask pointers stay put.

Fails closed (exit 1, no output line) when any current version cannot be read or is not a
stable semantic version, so no cask can be rewritten on missing evidence.
EOF
}

tap=""
source_repo=""
tag=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--tap)
		tap="${2:-}"
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
	--*)
		printf 'ERROR: unknown option %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	*) break ;;
	esac
done

repo_pattern='^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'
if [[ ! "${tap}" =~ ${repo_pattern} || ! "${source_repo}" =~ ${repo_pattern} || -z "${tag}" || $# -lt 1 ]]; then
	printf 'ERROR: --tap, --source-repo, --tag and at least one CASK_NAME are required\n' >&2
	usage >&2
	exit 2
fi

number='(0|[1-9][0-9]*)'
stable_pattern="^${number}\\.${number}\\.${number}$"

emit() {
	printf 'publish=%s\n' "$1"
	if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
		printf 'publish=%s\n' "$1" >>"${GITHUB_OUTPUT}"
	fi
}

# Prints -1, 0 or 1 for two stable versions already matched against stable_pattern.
compare_stable() {
	local -a left right
	local i
	IFS=. read -r -a left <<<"$1"
	IFS=. read -r -a right <<<"$2"
	for i in 0 1 2; do
		if ((10#${left[i]} < 10#${right[i]})); then
			printf -- '-1\n'
			return
		fi
		if ((10#${left[i]} > 10#${right[i]})); then
			printf '1\n'
			return
		fi
	done
	printf '0\n'
}

version="${tag#v}"
if [[ "${tag}" != v* ]]; then
	printf 'ERROR: tag %q does not start with v\n' "${tag}" >&2
	exit 1
fi
if [[ ! "${version}" =~ ${stable_pattern} ]]; then
	printf 'Tag %s is not a stable version; the shared cask pointers are left unchanged.\n' "${tag}" >&2
	emit false
	exit 0
fi

# Collect every current pointer before deciding, so a read failure anywhere blocks the decision.
references=()
labels=()

for cask in "$@"; do
	if [[ ! "${cask}" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
		printf 'ERROR: invalid cask name %q\n' "${cask}" >&2
		exit 2
	fi
	if ! content="$(gh api -H 'Accept: application/vnd.github.raw' \
		"repos/${tap}/contents/Casks/${cask}.rb")"; then
		printf 'ERROR: could not read Casks/%s.rb from %s\n' "${cask}" "${tap}" >&2
		exit 1
	fi
	versions="$(sed -nE 's/^[[:space:]]*version[[:space:]]+"([^"]*)"[[:space:]]*$/\1/p' <<<"${content}")"
	if [[ -z "${versions}" || "$(wc -l <<<"${versions}" | tr -d ' ')" != 1 ]]; then
		printf 'ERROR: Casks/%s.rb on %s does not declare exactly one version\n' "${cask}" "${tap}" >&2
		exit 1
	fi
	if [[ ! "${versions}" =~ ${stable_pattern} ]]; then
		printf 'ERROR: Casks/%s.rb on %s declares non-stable version %q\n' "${cask}" "${tap}" "${versions}" >&2
		exit 1
	fi
	references+=("${versions}")
	labels+=("cask ${cask}")
done

if ! latest="$(gh api "repos/${source_repo}/releases/latest" --jq '.tag_name')"; then
	printf 'ERROR: could not read the latest published release of %s\n' "${source_repo}" >&2
	exit 1
fi
latest_version="${latest#v}"
if [[ "${latest}" != v* || ! "${latest_version}" =~ ${stable_pattern} ]]; then
	printf 'ERROR: latest release of %s has non-stable tag %q\n' "${source_repo}" "${latest}" >&2
	exit 1
fi
references+=("${latest_version}")
labels+=("latest release")

for i in "${!references[@]}"; do
	if [[ "$(compare_stable "${version}" "${references[i]}")" == -1 ]]; then
		printf 'Tag %s is older than the %s (%s); the shared cask pointers are left unchanged.\n' \
			"${tag}" "${labels[i]}" "${references[i]}" >&2
		emit false
		exit 0
	fi
done

printf 'Tag %s is at least as new as every current pointer; the casks may be updated.\n' "${tag}" >&2
emit true
