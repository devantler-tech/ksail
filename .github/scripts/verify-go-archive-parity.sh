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

# The pin above is what this check compares against, and nothing tied it to what
# the module graph actually REQUIRES. A dependency bump to go.mod would leave
# this verifying the superseded version and still passing, while Go's metadata
# advertises the new one, so version-based vulnerability analysis would credit
# the binary with fixes that were never compiled. Assert the pin against every
# manifest that requires the module.
readonly module_manifests=(
	'go.mod'
	'desktop/go.mod'
)

require_manifest_pin() {
	local manifest="$1" path required count
	path="${repo_root}/${manifest}"
	[[ -f "${path}" ]] || {
		printf 'module manifest not found: %s\n' "${manifest}" >&2
		return 1
	}
	# `$2 ~ /^v[0-9]/` selects the require line and skips the `=>` replace
	# directive, which carries the same module path in $1.
	required="$(awk -v m="${module}" '$1 == m && $2 ~ /^v[0-9]/ { print $2 }' "${path}" | LC_ALL=C sort -u)"
	[[ -n "${required}" ]] || {
		printf '%s does not require %s, so the reviewed pin cannot be confirmed\n' "${manifest}" "${module}" >&2
		return 1
	}
	count="$(printf '%s\n' "${required}" | grep -c .)"
	((count == 1)) || {
		printf '%s requires %d versions of %s: %s\n' "${manifest}" "${count}" "${module}" "${required//$'\n'/ }" >&2
		return 1
	}
	[[ "${required}" == "${version}" ]] || {
		printf '%s requires %s %s but the reviewed parity pin is %s.\n' "${manifest}" "${module}" "${required}" "${version}" >&2
		printf 'Re-review the upstream source at %s, then update version and expected_sum in %s.\n' "${required}" "${0##*/}" >&2
		return 1
	}
}

manifest_status=0
for manifest in "${module_manifests[@]}"; do
	require_manifest_pin "${manifest}" || manifest_status=1
done
((manifest_status == 0)) || exit 1

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
# `-L` answers a question about the PATH AS WRITTEN, not about the directory the
# rest of this script goes on to enumerate. Bash dereferences a symbolic link
# whenever the tested path carries a trailing separator or a trailing `.`
# component, so `[[ -L "link/" ]]` and `[[ -L "link/." ]]` are both FALSE while
# `cd -- "link/"` resolves the link exactly as `cd -- "link"` does. A root
# spelled with either suffix therefore walked straight past this guard, and the
# comparison below enumerated the link's target — an unreviewed tree outside
# third_party/go-archive — while reporting parity.
#
# Strip those no-op suffixes first so the guard tests the same path `cd` will
# resolve. The loop is what makes it exhaustive rather than a list of spellings:
# it keeps removing trailing `/` and `/.` until neither is present, which folds
# `link//`, `link/./`, and `link/././` onto the same `link` as the bare form.
# The final component is preserved when it is the only one left, so a root of
# `/` is still tested as `/` rather than as the empty string.
normalize_root() {
	local path="$1"
	while [[ "${path}" == */ || "${path}" == */. ]]; do
		local stripped="${path%/}"
		stripped="${stripped%/.}"
		[[ -n "${stripped}" ]] || break
		path="${stripped}"
	done
	printf '%s' "${path}"
}

[[ ! -L "$(normalize_root "${upstream_dir}")" ]] || {
	printf 'symbolic link is not permitted for upstream module root\n' >&2
	exit 1
}
[[ ! -L "$(normalize_root "${local_dir}")" ]] || {
	printf 'symbolic link is not permitted for local module root\n' >&2
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
	local root="$1" rel raw_file status=0
	raw_file="$(mktemp)"
	# Capture the enumeration before filtering: a `find` failure must abort
	# rather than yield an empty list, which would read as "no differences".
	#
	# NUL-delimited, because a newline inside a path splits one entry into
	# several records that are then matched against the exception list
	# INDEPENDENTLY. `KSail-PATCH.md<LF>compat_legacy.go` disappears from the
	# comparison entirely — both halves are declared exceptions — while the Go
	# tool still compiles the real `.go` file. The enumeration cannot be held in
	# a variable: bash strips NUL bytes in command substitution, which would
	# silently rejoin every path into one.
	if ! (cd -- "${root}" && find . \( -type f -o -type l \) -print0) >"${raw_file}"; then
		rm -f "${raw_file}"
		return 1
	fi
	local -a kept=()
	# Deliberately not a pipeline: a `return` from inside a piped `while` runs
	# in a subshell and its status is discarded, so the rejection below would be
	# lost and the path would be treated as absent.
	while IFS= read -r -d '' rel; do
		[[ -n "${rel}" ]] || continue
		rel="${rel#./}"
		if [[ "${rel}" == *$'\n'* ]]; then
			printf 'newline in module path is not permitted: %q\n' "${rel}" >&2
			status=1
			break
		fi
		is_excepted "${rel}" || kept+=("${rel}")
	done <"${raw_file}"
	rm -f "${raw_file}"
	((status == 0)) || return 1
	((${#kept[@]} > 0)) || return 0
	printf '%s\n' "${kept[@]}" | LC_ALL=C sort
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

# The compatibility Go files are parity EXCEPTIONS, so `is_excepted()` removes
# them from both comparable-file lists and the shared-file guard below never
# examines them — yet they are compiled as part of the module. A link at either
# path therefore lets the Go tool build source from outside the provenance-checked
# tree, and that target can be repointed afterwards without this check seeing it.
# Being exempt from CONTENT parity is not the same as being exempt from having to
# live inside the module, so the file type is asserted here independently.
readonly compat_go_files=(
	'compat_legacy.go'
	'compat_legacy_test.go'
)
for compat_rel in "${compat_go_files[@]}"; do
	if [[ -L "${local_dir}/${compat_rel}" ]]; then
		printf 'symbolic link is not permitted at compatibility path: %s\n' "${compat_rel}" >&2
		status=1
	fi
done

if [[ -n "${shared_files}" ]]; then
	while IFS= read -r rel; do
		# `diff -u` FOLLOWS symbolic links, so identical bytes behind a link
		# read as parity. Enumeration admits links, so a local
		# `archive.go -> ../unreviewed/archive.go` compared clean while
		# RESOLVING OUTSIDE the parity-checked module — and its target could
		# then be changed without this check ever seeing it. Byte equality is
		# only a provenance guarantee when both sides are the same file TYPE,
		# so a link on either side is rejected before any content comparison.
		if [[ -L "${upstream_dir}/${rel}" || -L "${local_dir}/${rel}" ]]; then
			printf 'symbolic link is not permitted in the parity-checked module: %s\n' "${rel}" >&2
			status=1
			continue
		fi
		diff -u "${upstream_dir}/${rel}" "${local_dir}/${rel}" || status=1
	done <<<"${shared_files}"
fi

((status == 0)) || {
	printf 'go-archive %s parity validation failed\n' "${version}" >&2
	exit 1
}

printf 'go-archive %s parity verified at %s\n' "${version}" "${expected_sum}"
