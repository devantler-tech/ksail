#!/usr/bin/env bash

set -euo pipefail

readonly module='github.com/moby/go-archive'
readonly version='v0.3.0'
readonly expected_sum='h1:nos4BtzzUIqB406BgQnWGMI4qib9BZ8XUHU+ucv/n1c='
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
local_dir="${repo_root}/third_party/go-archive"
upstream_dir=''

usage() {
	printf 'Usage: %s [--upstream-dir DIR] [--local-dir DIR]\n' "${0##*/}" >&2
}

while (($# > 0)); do
	case "$1" in
	--upstream-dir)
		[[ $# -ge 2 ]] || {
			usage
			exit 2
		}
		upstream_dir="$2"
		shift 2
		;;
	--local-dir)
		[[ $# -ge 2 ]] || {
			usage
			exit 2
		}
		local_dir="$2"
		shift 2
		;;
	*)
		usage
		exit 2
		;;
	esac
done

if [[ -z "${upstream_dir}" ]]; then
	module_json="$(go mod download -json "${module}@${version}")"
	upstream_dir="$({
		jq -er --arg expected "${expected_sum}" \
			'select(.Sum == $expected and (.Error // "") == "") | .Dir' <<<"${module_json}"
	} 2>/dev/null)" || {
		printf 'go-archive download did not resolve the reviewed checksum %s\n' "${expected_sum}" >&2
		exit 1
	}
fi

[[ -d "${upstream_dir}" ]] || {
	printf 'upstream module directory does not exist: %s\n' "${upstream_dir}" >&2
	exit 1
}
[[ -d "${local_dir}" ]] || {
	printf 'local module directory does not exist: %s\n' "${local_dir}" >&2
	exit 1
}

# Go's module zip carries repository metadata that KSail deliberately does not
# import, and KSail adds a small compatibility surface of its own. Both sets are
# declared here as EXACT top-level entries: an exception covers that entry and,
# when it is a directory, everything beneath it.
#
# Anchoring is the point. `diff --exclude=PATTERN` matches a BASENAME at every
# recursion level, so a nested `pkg/compat_legacy.go` or `pkg/.github/` was
# silently exempted and could carry undeclared code into the vendored copy.
readonly parity_exceptions=(
	'.gitattributes'
	'.github'
	'.gitignore'
	'.golangci.yml'
	'KSail-PATCH.md'
	'compat_legacy.go'
	'compat_legacy_test.go'
)

is_excepted() {
	local rel="$1" exception
	for exception in "${parity_exceptions[@]}"; do
		[[ "${rel}" == "${exception}" || "${rel}" == "${exception}/"* ]] && return 0
	done
	return 1
}

# Capture the enumeration before filtering: a `find` failure must abort rather
# than yield an empty list, which would read as "no differences".
list_comparable_files() {
	local root="$1" raw rel
	raw="$(cd -- "${root}" && find . \( -type f -o -type l \) -print)" || return 1
	while IFS= read -r rel; do
		[[ -n "${rel}" ]] || continue
		rel="${rel#./}"
		is_excepted "${rel}" || printf '%s\n' "${rel}"
	done <<<"${raw}" | LC_ALL=C sort
}

list_dir="$(mktemp -d)"
trap 'rm -rf "${list_dir}"' EXIT
list_comparable_files "${upstream_dir}" >"${list_dir}/upstream"
list_comparable_files "${local_dir}" >"${list_dir}/local"

# An empty upstream enumeration is UNKNOWN, never clean.
[[ -s "${list_dir}/upstream" ]] || {
	printf 'no comparable files found under %s; refusing to report parity\n' "${upstream_dir}" >&2
	exit 1
}

# Command substitution so a `comm` failure aborts instead of looking clean.
missing_locally="$(LC_ALL=C comm -23 "${list_dir}/upstream" "${list_dir}/local")"
undeclared_locally="$(LC_ALL=C comm -13 "${list_dir}/upstream" "${list_dir}/local")"
shared_files="$(LC_ALL=C comm -12 "${list_dir}/upstream" "${list_dir}/local")"

status=0

if [[ -n "${missing_locally}" ]]; then
	status=1
	while IFS= read -r rel; do
		printf 'missing from local copy: %s\n' "${rel}" >&2
	done <<<"${missing_locally}"
fi

if [[ -n "${undeclared_locally}" ]]; then
	status=1
	while IFS= read -r rel; do
		printf 'undeclared in local copy: %s\n' "${rel}" >&2
	done <<<"${undeclared_locally}"
fi

if [[ -n "${shared_files}" ]]; then
	while IFS= read -r rel; do
		diff -u "${upstream_dir}/${rel}" "${local_dir}/${rel}" || status=1
	done <<<"${shared_files}"
fi

((status == 0)) || {
	printf 'go-archive %s parity validation failed\n' "${version}" >&2
	exit 1
}

printf 'go-archive %s parity verified at %s\n' "${version}" "${expected_sum}"
