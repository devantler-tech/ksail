#!/usr/bin/env sh

set -eu

readonly repository='devantler-tech/ksail'
readonly release_base_url="${KSAIL_RELEASE_BASE_URL:-https://github.com/${repository}/releases}"

fail() {
	printf '%s\n' "$1" >&2
	exit 1
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		checksum_output="$(sha256sum "$1")" || return 1
	elif command -v shasum >/dev/null 2>&1; then
		checksum_output="$(shasum -a 256 "$1")" || return 1
	else
		fail 'sha256sum or shasum is required to verify the KSail release'
	fi
	printf '%s\n' "${checksum_output}" | awk '{print $1}'
}

version="${KSAIL_VERSION:-latest}"
if [ "${version}" = 'latest' ]; then
	latest_url="$(
		curl --fail --silent --show-error --location --head \
			--output /dev/null --write-out '%{url_effective}' \
			"${release_base_url}/latest"
	)"
	version="${latest_url##*/}"
fi

if ! printf '%s\n' "${version}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$'; then
	printf 'invalid KSail release version: %s\n' "${version}" >&2
	exit 1
fi

case "$(uname -s)" in
Darwin) operating_system='darwin' ;;
Linux) operating_system='linux' ;;
*)
	printf 'unsupported operating system: %s\n' "$(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) architecture='amd64' ;;
arm64 | aarch64) architecture='arm64' ;;
*)
	printf 'unsupported architecture: %s\n' "$(uname -m)" >&2
	exit 1
	;;
esac

version_number="${version#v}"
asset="ksail_${version_number}_${operating_system}_${architecture}.tar.gz"
checksums="ksail_${version_number}_checksums.txt"

tmp_dir="$(mktemp -d)"
stage_dir=''
notice_dir=''
notice_stage=''
reuse_notices=false
cleanup() {
	status=$?
	trap - 0 HUP INT TERM
	rm -rf "${tmp_dir}"
	if [ -n "${stage_dir}" ]; then rm -rf "${stage_dir}"; fi
	if [ -n "${notice_stage}" ]; then rm -rf "${notice_stage}"; fi
	exit "${status}"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

archive_path="${tmp_dir}/${asset}"
checksums_path="${tmp_dir}/${checksums}"
download_base="${release_base_url}/download/${version}"

curl --fail --silent --show-error --location \
	--output "${archive_path}" "${download_base}/${asset}"
curl --fail --silent --show-error --location \
	--output "${checksums_path}" "${download_base}/${checksums}"

expected_checksum="$(
	awk -v target="${asset}" '
		{
			name = $2
			sub(/^\*/, "", name)
			if (name == target) {
				print $1
			}
		}
	' "${checksums_path}"
)"
if ! printf '%s\n' "${expected_checksum}" | grep -Eq '^[0-9a-fA-F]{64}$'; then
	printf 'release checksums do not contain %s\n' "${asset}" >&2
	exit 1
fi

actual_checksum="$(sha256 "${archive_path}")"

if [ "${actual_checksum}" != "${expected_checksum}" ]; then
	printf 'checksum mismatch for %s\n' "${asset}" >&2
	exit 1
fi

# Never extract archive paths onto the filesystem. Admit each exact member once,
# verify its regular-file type, then stream its contents into our private directory.
tar -P -tzf "${archive_path}" >"${tmp_dir}/members"
if ! awk '/^\// || /(^|\/)\.\.(\/|$)/ { exit 1 }' "${tmp_dir}/members"; then
	fail 'release archive contains an unsafe path'
fi
mkdir "${tmp_dir}/GoNotices"
for member in ksail GoNotices/THIRD_PARTY_NOTICES.txt GoNotices/manifest.json GoNotices/release.txt; do
	if ! awk -v member="${member}" '$0 == member { count++ } END { exit count != 1 }' "${tmp_dir}/members"; then
		fail "release archive is missing a unique required member: ${member}"
	fi
	tar -tvzf "${archive_path}" -- "${member}" >"${tmp_dir}/member-type"
	if ! awk 'NR != 1 || substr($0, 1, 1) != "-" { exit 1 } END { if (NR != 1) exit 1 }' "${tmp_dir}/member-type"; then
		fail 'release archive required member is not a regular file'
	fi
	tar -xOzf "${archive_path}" -- "${member}" >"${tmp_dir}/${member}"
	if [ ! -s "${tmp_dir}/${member}" ]; then
		fail "release archive required member is empty: ${member}"
	fi
done

# The producer verifies the source inventory. Recipients bind its opaque JSON and
# notice text to the selected release and target without another runtime/parser.
source_commit="$(sed -n '3s/^source=//p' "${tmp_dir}/GoNotices/release.txt")"
if ! printf '%s\n' "${source_commit}" | grep -Eq '^[0-9a-f]{40}$' ||
	[ "${source_commit}" = '0000000000000000000000000000000000000000' ]; then
	fail 'release notices have an invalid source binding'
fi
target="cli-${operating_system}-${architecture}"
notices_checksum="$(sha256 "${tmp_dir}/GoNotices/THIRD_PARTY_NOTICES.txt")"
manifest_checksum="$(sha256 "${tmp_dir}/GoNotices/manifest.json")"
printf '%s\n' 'KSAIL-GO-NOTICES-1' "version=${version}" "source=${source_commit}" "target=${target}" \
	"notices_sha256=${notices_checksum}" "manifest_sha256=${manifest_checksum}" >"${tmp_dir}/expected-binding"
if ! cmp -s "${tmp_dir}/expected-binding" "${tmp_dir}/GoNotices/release.txt"; then
	fail 'release notices do not match the selected release, target or content'
fi

install_dir="${KSAIL_INSTALL_DIR:-}"
if [ -z "${install_dir}" ]; then
	if command -v go >/dev/null 2>&1; then
		install_dir="$(go env GOPATH)/bin"
	else
		install_dir="${HOME}/.local/bin"
	fi
fi

case "${install_dir}" in /*) ;; *) install_dir="./${install_dir}" ;; esac
mkdir -p "${install_dir}"
install_dir="$(cd -P "${install_dir}" && pwd -P)"
if [ -L "${install_dir}/ksail" ] || { [ -e "${install_dir}/ksail" ] && [ ! -f "${install_dir}/ksail" ]; }; then
	fail 'existing KSail destination is not a regular file'
fi
notice_root="${install_dir}/.ksail-notices"
if [ -L "${notice_root}" ] || { [ -e "${notice_root}" ] && [ ! -d "${notice_root}" ]; }; then
	fail 'existing KSail notices root is not a directory'
fi
if [ ! -d "${notice_root}" ]; then mkdir -m 0755 "${notice_root}"; fi
notice_dir="${notice_root}/${version}-${target}"
if [ -L "${notice_dir}" ] || { [ -e "${notice_dir}" ] && [ ! -d "${notice_dir}" ]; }; then
	fail 'existing KSail version notices are not an ordinary directory'
fi
if [ -d "${notice_dir}" ]; then
	# A same-version reinstall may reuse only the exact retained regular bundle.
	# Never write into or remove an existing directory, even on a later failure.
	if [ ! -r "${notice_dir}" ] || [ ! -x "${notice_dir}" ]; then
		fail 'existing KSail version notices cannot be inspected completely'
	fi
	for member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
		if [ -L "${notice_dir}/${member}" ] || [ ! -f "${notice_dir}/${member}" ]; then
			fail 'existing KSail version notices are not regular files'
		fi
		ls -ld "${notice_dir}/${member}" >"${tmp_dir}/retained-type"
		if ! awk '{ exit $2 != 1 }' "${tmp_dir}/retained-type" ||
			! cmp -s "${tmp_dir}/GoNotices/${member}" "${notice_dir}/${member}"; then
			fail 'existing KSail version notices conflict with the release'
		fi
	done
	for retained in "${notice_dir}"/* "${notice_dir}"/.[!.]* "${notice_dir}"/..?*; do
		if [ ! -e "${retained}" ] && [ ! -L "${retained}" ]; then continue; fi
		case "${retained##*/}" in
		THIRD_PARTY_NOTICES.txt | manifest.json | release.txt) ;;
		*) fail 'existing KSail version notices contain unexpected entries' ;;
		esac
	done
	reuse_notices=true
fi
stage_dir="$(mktemp -d "${install_dir}/.ksail-install.XXXXXX")"
if command -v install >/dev/null 2>&1; then
	install -m 0755 "${tmp_dir}/ksail" "${stage_dir}/ksail"
else
	cp "${tmp_dir}/ksail" "${stage_dir}/ksail"
	chmod 0755 "${stage_dir}/ksail"
fi
if [ "${reuse_notices}" = false ]; then
	# Publish only a complete bundle. A signal during private staging cannot
	# leave an empty public version reservation that blocks a later retry.
	notice_stage="$(mktemp -d "${notice_root}/.ksail-notices-stage.XXXXXX")"
	bundle="${notice_stage}/${version}-${target}"
	mkdir -m 0755 "${bundle}"
	for member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
		cp "${tmp_dir}/GoNotices/${member}" "${bundle}/${member}"
		chmod 0644 "${bundle}/${member}"
	done
	# Both directories are on the notices filesystem. Never replace a concurrent
	# destination, even an empty one; a skipped move must refuse binary publication.
	mv -n "${bundle}" "${notice_root}/"
	if [ -e "${bundle}" ] || [ -L "${bundle}" ]; then
		fail 'KSail version notices appeared during installation; retry after inspection'
	fi
fi
# The staged executable shares the destination filesystem. All validation and
# required notice writes succeeded before this atomic binary replacement.
# Published bundles remain intact on signals or an ambiguous binary rename.
mv -f "${stage_dir}/ksail" "${install_dir}/ksail"

printf 'Installed KSail %s to %s/ksail\n' "${version}" "${install_dir}"
printf 'Retained Go dependency notices at %s\n' "${notice_dir}"
case ":${PATH}:" in
*":${install_dir}:"*) ;;
*) printf 'Add %s to PATH to run ksail.\n' "${install_dir}" ;;
esac
