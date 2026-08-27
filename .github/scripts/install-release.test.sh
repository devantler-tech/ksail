#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
installer="${repo_root}/install.sh"

[[ -x "${installer}" ]] || {
	printf 'FAIL: release installer is missing or not executable: %s\n' "${installer}" >&2
	exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fixtures="${tmp_dir}/fixtures"
fake_bin="${tmp_dir}/bin"
install_dir="${tmp_dir}/installed"
payload_dir="${tmp_dir}/payload"
mkdir -p "${fixtures}" "${fake_bin}" "${install_dir}" "${payload_dir}"

printf '#!/usr/bin/env sh\nprintf "ksail test binary\\n"\n' >"${payload_dir}/ksail"
chmod 0755 "${payload_dir}/ksail"

asset='ksail_7.180.5_linux_amd64.tar.gz'
tar -czf "${fixtures}/${asset}" -C "${payload_dir}" ksail
checksum="$(sha256sum "${fixtures}/${asset}" | awk '{print $1}')"
printf '%s  %s\n' "${checksum}" "${asset}" >"${fixtures}/ksail_7.180.5_checksums.txt"

# The single-quoted lines below intentionally write an unevaluated fixture script.
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/usr/bin/env sh' \
	'set -eu' \
	'case "${1:-}" in' \
	'  -s) printf "%s\\n" "${KSAIL_TEST_OS:-Linux}" ;;' \
	'  -m) printf "%s\\n" "${KSAIL_TEST_ARCH:-x86_64}" ;;' \
	'  *) exit 2 ;;' \
	'esac' >"${fake_bin}/uname"
chmod 0755 "${fake_bin}/uname"

# The single-quoted lines below intentionally write an unevaluated fixture script.
# shellcheck disable=SC2016
printf '%s\n' \
	'#!/usr/bin/env sh' \
	'set -eu' \
	'output=""' \
	'write_out=""' \
	'url=""' \
	'while [ "$#" -gt 0 ]; do' \
	'  case "$1" in' \
	'    --output) output="$2"; shift 2 ;;' \
	'    --write-out) write_out="$2"; shift 2 ;;' \
	'    --fail|--silent|--show-error|--location|--head) shift ;;' \
	'    *) url="$1"; shift ;;' \
	'  esac' \
	'done' \
	'if [ -n "$write_out" ]; then' \
	'  printf "%s" "${KSAIL_TEST_LATEST_URL:?}"' \
	'  exit 0' \
	'fi' \
	'cp "${KSAIL_TEST_FIXTURES:?}/${url##*/}" "$output"' >"${fake_bin}/curl"
chmod 0755 "${fake_bin}/curl"

run_installer() {
	env \
		PATH="${fake_bin}:${PATH}" \
		KSAIL_INSTALL_DIR="${install_dir}" \
		KSAIL_RELEASE_BASE_URL='https://example.invalid/releases' \
		KSAIL_TEST_FIXTURES="${fixtures}" \
		KSAIL_TEST_LATEST_URL='https://example.invalid/releases/tag/v7.180.5' \
		"${installer}"
}

KSAIL_VERSION='v7.180.5' run_installer
[[ -x "${install_dir}/ksail" ]] || {
	printf 'FAIL: installer did not create an executable ksail binary\n' >&2
	exit 1
}
[[ "$("${install_dir}/ksail")" == 'ksail test binary' ]] || {
	printf 'FAIL: installed binary content did not match the verified archive\n' >&2
	exit 1
}

rm -f "${install_dir}/ksail"
KSAIL_VERSION='latest' run_installer
[[ -x "${install_dir}/ksail" ]] || {
	printf 'FAIL: latest release resolution did not install ksail\n' >&2
	exit 1
}

printf '%064d  %s\n' 0 "${asset}" >"${fixtures}/ksail_7.180.5_checksums.txt"
if KSAIL_VERSION='v7.180.5' run_installer >/dev/null 2>&1; then
	printf 'FAIL: archive with a mismatched checksum was installed\n' >&2
	exit 1
fi

if KSAIL_VERSION='v7.180.5' KSAIL_TEST_ARCH='s390x' run_installer >/dev/null 2>&1; then
	printf 'FAIL: unsupported architecture was accepted\n' >&2
	exit 1
fi

if KSAIL_VERSION='v7.180.5/../../malicious' run_installer >/dev/null 2>&1; then
	printf 'FAIL: malformed release version was accepted\n' >&2
	exit 1
fi

release_workflow="${repo_root}/.github/workflows/cd.yaml"
for release_guard in \
	'const requiredInstallerAssets = [' \
	"'install.sh'," \
	'missing required installer release assets'; do
	if ! grep -Fq "${release_guard}" "${release_workflow}"; then
		printf 'FAIL: release workflow does not enforce %s\n' "${release_guard}" >&2
		exit 1
	fi
done

printf 'All release installer cases passed.\n'
