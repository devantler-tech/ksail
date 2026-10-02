#!/usr/bin/env bash
set -euo pipefail

readonly module='github.com/uptrace/opentelemetry-go-extra/otelzap'
readonly version='v0.3.2'
readonly checksum='h1:cj/Z6FKTTYBnstI0Lni9PA+k2foounKIPUmj1LBwNiQ='
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd -P)"
local_dir="${repo_root}/third_party/otelzap"
patch_file="${script_dir}/../patches/otelzap-v0.3.2.patch"
upstream_dir=''

# Overrides support isolated, network-free regression fixtures. CI uses the fixed defaults.
while (($#)); do
	[[ $# -ge 2 ]] || exit 2
	case "$1" in
	--upstream-dir) upstream_dir="$2" ;;
	--local-dir) local_dir="$2" ;;
	--patch-file) patch_file="$2" ;;
	*) exit 2 ;;
	esac
	shift 2
done

metadata="$(GOFLAGS='' GOENV=off GOWORK=off go list -m -json "${module}")"
jq -e --arg module "${module}" --arg version "${version}" --arg dir "${repo_root}/third_party/otelzap" \
	'.Path == $module and .Version == $version and .Replace.Dir == $dir' <<<"${metadata}" >/dev/null || {
	printf 'otelzap is not supplied by the reviewed local replacement\n' >&2
	exit 1
}
effective_dir="$(jq -er '.Replace.Dir' <<<"${metadata}")"
[[ "$(cd -- "${effective_dir}" && pwd -P)" == "${repo_root}/third_party/otelzap" ]] || {
	printf 'the selected adapter resolves outside the reviewed module directory\n' >&2
	exit 1
}

if [[ -z "${upstream_dir}" ]]; then
	resolved="$(go mod download -json "${module}@${version}")"
	upstream_dir="$(jq -er --arg sum "${checksum}" \
		'select(.Sum == $sum and (.Error // "") == "") | .Dir' <<<"${resolved}")"
fi

# Resolve the actual directory before rejecting a linked root. Appended slash/dot
# components must not turn a symlink into an apparently ordinary directory.
reject_links() {
	local root="$1" links
	while [[ "${root}" == */ || "${root}" == */. ]]; do
		root="${root%/}"
		root="${root%/.}"
	done
	[[ -d "${root}" && ! -L "${root}" ]] || {
		printf 'module root must be an ordinary directory\n' >&2
		return 1
	}
	links="$(find "${root}" -type l -print)"
	[[ -z "${links}" ]] || {
		printf 'symbolic links are not permitted in the compatibility module\n' >&2
		return 1
	}
}

reject_links "${upstream_dir}"
reject_links "${local_dir}"
[[ -s "${upstream_dir}/go.mod" && -s "${upstream_dir}/LICENSE" ]] || {
	printf 'upstream module is incomplete; refusing to report provenance\n' >&2
	exit 1
}
[[ -f "${patch_file}" && ! -L "${patch_file}" ]] || exit 1

temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT
cp -R "${upstream_dir}/." "${temporary}/expected"
chmod -R u+w "${temporary}/expected"
patch --batch --forward -d "${temporary}/expected" -p1 <"${patch_file}"
cp -R "${local_dir}/." "${temporary}/actual"
chmod -R u+w "${temporary}/actual"

# Only this top-level prose note is outside byte parity. A similarly named nested
# source remains visible to diff, as do every extra/missing file and directory.
rm -f "${temporary}/actual/KSail-PATCH.md"
diff -ru "${temporary}/expected" "${temporary}/actual"
printf 'otelzap %s verified against checksum %s plus the reviewed patch\n' "${version}" "${checksum}"
