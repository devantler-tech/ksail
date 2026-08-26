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

# Go's module zip contains repository metadata that KSail deliberately does not
# import. Every distributable module file must otherwise remain byte-identical;
# the three named files are the complete KSail-owned compatibility surface.
diff -ruN \
	--exclude='.gitattributes' \
	--exclude='.github' \
	--exclude='.gitignore' \
	--exclude='.golangci.yml' \
	--exclude='KSail-PATCH.md' \
	--exclude='compat_legacy.go' \
	--exclude='compat_legacy_test.go' \
	"${upstream_dir}" "${local_dir}"

printf 'go-archive %s parity verified at %s\n' "${version}" "${expected_sum}"
