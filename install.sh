#!/usr/bin/env sh

set -eu

readonly repository='devantler-tech/ksail'
readonly release_base_url="${KSAIL_RELEASE_BASE_URL:-https://github.com/${repository}/releases}"

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
trap 'rm -rf "${tmp_dir}"' 0 HUP INT TERM

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

if command -v sha256sum >/dev/null 2>&1; then
	actual_checksum="$(sha256sum "${archive_path}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
	actual_checksum="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
else
	printf 'sha256sum or shasum is required to verify the KSail release\n' >&2
	exit 1
fi

if [ "${actual_checksum}" != "${expected_checksum}" ]; then
	printf 'checksum mismatch for %s\n' "${asset}" >&2
	exit 1
fi

tar -xzf "${archive_path}" -C "${tmp_dir}" ksail

install_dir="${KSAIL_INSTALL_DIR:-}"
if [ -z "${install_dir}" ]; then
	if command -v go >/dev/null 2>&1; then
		install_dir="$(go env GOPATH)/bin"
	else
		install_dir="${HOME}/.local/bin"
	fi
fi

mkdir -p "${install_dir}"
if command -v install >/dev/null 2>&1; then
	install -m 0755 "${tmp_dir}/ksail" "${install_dir}/ksail"
else
	cp "${tmp_dir}/ksail" "${install_dir}/ksail"
	chmod 0755 "${install_dir}/ksail"
fi

printf 'Installed KSail %s to %s/ksail\n' "${version}" "${install_dir}"
case ":${PATH}:" in
*":${install_dir}:"*) ;;
*) printf 'Add %s to PATH to run ksail.\n' "${install_dir}" ;;
esac
