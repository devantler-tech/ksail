#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
validator="${script_dir}/verify-go-archive-parity.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

upstream="${tmp_dir}/upstream"
local_copy="${tmp_dir}/local"
mkdir -p "${upstream}/.github" "${local_copy}"
printf 'package archive\n' >"${upstream}/archive.go"
printf 'ignored repository metadata\n' >"${upstream}/.github/workflow.yml"
cp "${upstream}/archive.go" "${local_copy}/archive.go"
printf 'package archive\n' >"${local_copy}/compat_legacy.go"
printf 'package archive\n' >"${local_copy}/compat_legacy_test.go"
printf '# Local patch\n' >"${local_copy}/KSail-PATCH.md"

"${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}"

printf 'package changed\n' >"${local_copy}/archive.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: modified upstream source passed parity validation\n' >&2
	exit 1
fi

cp "${upstream}/archive.go" "${local_copy}/archive.go"
printf 'package unexpected\n' >"${local_copy}/unexpected.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: unexpected local source passed parity validation\n' >&2
	exit 1
fi

rm "${local_copy}/unexpected.go"
printf 'module runtime data\n' >"${upstream}/.runtime"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: missing hidden module file passed parity validation\n' >&2
	exit 1
fi

printf 'All go-archive parity cases passed.\n'
