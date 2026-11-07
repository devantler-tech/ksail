#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
installer="${repo_root}/install.sh"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

digest() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

write_binding() {
	printf '%s\n' 'KSAIL-GO-NOTICES-1' "version=${version}" \
		'source=1111111111111111111111111111111111111111' 'target=cli-linux-amd64' \
		"notices_sha256=$(digest "${payload}/GoNotices/THIRD_PARTY_NOTICES.txt")" \
		"manifest_sha256=$(digest "${payload}/GoNotices/manifest.json")" >"${payload}/GoNotices/release.txt"
}

make_archive() {
	local member
	local members=()
	for member in ksail GoNotices/THIRD_PARTY_NOTICES.txt GoNotices/manifest.json GoNotices/release.txt; do
		if [[ -e "${payload}/${member}" || -L "${payload}/${member}" ]]; then
			members+=("${member}")
		fi
	done
	tar -cf "${work}/release.tar" -C "${payload}" "${members[@]}"
	case "${test_case}" in
	duplicate-binary | duplicate-notices | duplicate-manifest | duplicate-binding)
		tar -rf "${work}/release.tar" -C "${payload}" "${duplicate_member}"
		;;
	traversal) tar -P -rf "${work}/release.tar" -C "${payload}" ../escape ;;
	absolute) tar -P -rf "${work}/release.tar" "${work}/escape" ;;
	esac
	gzip -c "${work}/release.tar" >"${fixtures}/${asset}"
	printf '%s  %s\n' "$(digest "${fixtures}/${asset}")" "${asset}" >"${fixtures}/${checksums}"
}

make_stubs() {
	cat >"${fake_bin}/uname" <<'STUB'
#!/usr/bin/env sh
set -eu
case "${1:-}" in
-s) printf '%s\n' "${KSAIL_TEST_OS:-Linux}" ;;
-m) printf '%s\n' "${KSAIL_TEST_ARCH:-x86_64}" ;;
*) exit 2 ;;
esac
STUB
	cat >"${fake_bin}/curl" <<'STUB'
#!/usr/bin/env sh
set -eu
output=''; write_out=''; url=''
while [ "$#" -gt 0 ]; do
  case "$1" in
  --output) output="$2"; shift 2 ;;
  --write-out) write_out="$2"; shift 2 ;;
  --fail|--silent|--show-error|--location|--head) shift ;;
  *) url="$1"; shift ;;
  esac
done
case "$url" in https://example.invalid/releases/*) ;; *) exit 98 ;; esac
if [ -n "$write_out" ]; then
  printf '%s' 'https://example.invalid/releases/tag/v7.180.5'
else
  "$KSAIL_TEST_CP" "$KSAIL_TEST_FIXTURES/${url##*/}" "$output"
fi
STUB
	cat >"${fake_bin}/cp" <<'STUB'
#!/usr/bin/env sh
set -eu
case "${KSAIL_TEST_FAULT:-}:$*" in
copy-notices:*manifest.json*) exit 81 ;;
esac
exec "$KSAIL_TEST_CP" "$@"
STUB
	cat >"${fake_bin}/install" <<'STUB'
#!/usr/bin/env sh
set -eu
[ "${KSAIL_TEST_FAULT:-}" != copy-binary ] || exit 82
if [ "${KSAIL_TEST_FAULT:-}" = race-early-directory ]; then
  "$KSAIL_TEST_INSTALL" "$@"
  "$KSAIL_TEST_MKDIR" "$KSAIL_TEST_NOTICE_DIR"
  printf 'concurrent\n' >"$KSAIL_TEST_NOTICE_DIR/retained"
  ls -din "$KSAIL_TEST_NOTICE_DIR" >"$KSAIL_TEST_RACE_ID"
  exit 0
fi
exec "$KSAIL_TEST_INSTALL" "$@"
STUB
	cat >"${fake_bin}/mv" <<'STUB'
#!/usr/bin/env sh
set -eu
for destination; do :; done
case "$destination" in
*/.ksail-notices/)
  case "${KSAIL_TEST_FAULT:-}" in
  race-empty | race-conflict)
    "$KSAIL_TEST_MKDIR" "$KSAIL_TEST_NOTICE_DIR"
    if [ "$KSAIL_TEST_FAULT" = race-conflict ]; then
      printf 'concurrent\n' >"$KSAIL_TEST_NOTICE_DIR/retained"
    fi
    ls -din "$KSAIL_TEST_NOTICE_DIR" >"$KSAIL_TEST_RACE_ID"
    ;;
  race-symlink)
    "$KSAIL_TEST_MKDIR" "$KSAIL_TEST_NOTICE_DIR-outside"
    printf 'concurrent\n' >"$KSAIL_TEST_NOTICE_DIR-outside/retained"
    ln -s "$KSAIL_TEST_NOTICE_DIR-outside" "$KSAIL_TEST_NOTICE_DIR"
    ls -din "$KSAIL_TEST_NOTICE_DIR" >"$KSAIL_TEST_RACE_ID"
    ;;
  esac
  ;;
*/ksail)
  [ "${KSAIL_TEST_FAULT:-}" != final-rename ] || exit 83
  case "${KSAIL_TEST_FAULT:-}" in
  signal-after-rename-*)
    "$KSAIL_TEST_MV" "$@"
    kill -"${KSAIL_TEST_FAULT##*-}" "$PPID"
    exit 0
    ;;
  esac
  ;;
esac
exec "$KSAIL_TEST_MV" "$@"
STUB
	cat >"${fake_bin}/mkdir" <<'STUB'
#!/usr/bin/env sh
set -eu
"$KSAIL_TEST_MKDIR" "$@"
for destination; do :; done
case "$destination:${KSAIL_TEST_FAULT:-}" in
*/v7.180.5-cli-linux-amd64:signal-after-reservation-*)
  kill -"${KSAIL_TEST_FAULT##*-}" "$PPID"
  ;;
esac
STUB
	if [[ "${test_case}" == hash-read-failure ]]; then
		cat >"${fake_bin}/sha256sum" <<'STUB'
#!/usr/bin/env sh
set -eu
case "$1" in */GoNotices/THIRD_PARTY_NOTICES.txt) exit 84 ;; esac
if [ "$KSAIL_TEST_SHA_KIND" = sha256sum ]; then
  exec "$KSAIL_TEST_SHA" "$@"
else
  exec "$KSAIL_TEST_SHA" -a 256 "$@"
fi
STUB
	fi
	chmod 0755 "${fake_bin}/"*
}

run_installer() {
	env PATH="${fake_bin}:${PATH}" TMPDIR="${work}/temporary" \
		KSAIL_INSTALL_DIR="${install_dir}" KSAIL_VERSION="${requested_version}" \
		KSAIL_RELEASE_BASE_URL='https://example.invalid/releases' \
		KSAIL_TEST_FIXTURES="${fixtures}" KSAIL_TEST_FAULT="${fault}" \
		KSAIL_TEST_CP="${real_cp}" KSAIL_TEST_INSTALL="${real_install}" KSAIL_TEST_MV="${real_mv}" \
		KSAIL_TEST_MKDIR="${real_mkdir}" \
		KSAIL_TEST_NOTICE_DIR="${notice_dir}" KSAIL_TEST_RACE_ID="${work}/race-id" \
		KSAIL_TEST_SHA="${real_sha}" KSAIL_TEST_SHA_KIND="${real_sha_kind}" \
		KSAIL_TEST_ARCH="${test_arch}" "${KSAIL_INSTALL_TEST_SHELL:-sh}" "${installer}"
}

run_case() {
	local test_case="$1" fixtures fake_bin install_dir payload version requested_version
	local asset checksums notice_dir old_notice real_cp real_install real_mv test_arch duplicate_member
	local real_sha real_sha_kind real_mkdir
	local fault="${test_case}" retained_identity binary_identity retained_bytes
	work="$(mktemp -d "${TMPDIR:-/tmp}/ksail-installer-test.XXXXXX")"
	work="$(cd "${work}" && pwd -P)"
	trap 'chmod -R u+rwX "${work}" 2>/dev/null || true; rm -rf "${work}"' EXIT
	fixtures="${work}/fixtures"
	fake_bin="${work}/bin"
	install_dir="${work}/installed"
	payload="${work}/payload"
	version='v7.180.5'
	requested_version="${version}"
	test_arch=x86_64
	asset='ksail_7.180.5_linux_amd64.tar.gz'
	checksums='ksail_7.180.5_checksums.txt'
	notice_dir="${install_dir}/.ksail-notices/v7.180.5-cli-linux-amd64"
	old_notice="${install_dir}/.ksail-notices/v7.180.4-cli-linux-amd64"
	real_cp="$(command -v cp)"
	real_install="$(command -v install)"
	real_mv="$(command -v mv)"
	real_mkdir="$(command -v mkdir)"
	if command -v sha256sum >/dev/null 2>&1; then
		real_sha="$(command -v sha256sum)"
		real_sha_kind=sha256sum
	else
		real_sha="$(command -v shasum)"
		real_sha_kind=shasum
	fi
	mkdir -p "${fixtures}" "${fake_bin}" "${payload}/GoNotices" "${old_notice}" "${work}/temporary"
	printf 'previous binary\n' >"${install_dir}/ksail"
	printf 'previous notices\n' >"${old_notice}/retained"
	printf '#!/usr/bin/env sh\nprintf "ksail test binary\\n"\n' >"${payload}/ksail"
	chmod 0755 "${payload}/ksail"
	printf 'Recipient notices\n' >"${payload}/GoNotices/THIRD_PARTY_NOTICES.txt"
	printf '{"opaque":"upstream verified inventory"}\n' >"${payload}/GoNotices/manifest.json"
	printf 'outside archive member\n' >"${work}/escape"
	write_binding
	case "${test_case}" in
	latest) requested_version=latest ;;
	missing-notices) rm "${payload}/GoNotices/THIRD_PARTY_NOTICES.txt" ;;
	missing-manifest) rm "${payload}/GoNotices/manifest.json" ;;
	missing-binding) rm "${payload}/GoNotices/release.txt" ;;
	stale-version)
		sed 's/version=v7.180.5/version=v7.180.4/' "${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	wrong-target)
		sed 's/cli-linux-amd64/cli-linux-arm64/' "${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	zero-source)
		sed 's/1111111111111111111111111111111111111111/0000000000000000000000000000000000000000/' \
			"${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	malformed-source)
		sed 's/source=.*/source=ABC/' "${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	notices-hash) printf 'changed notices\n' >>"${payload}/GoNotices/THIRD_PARTY_NOTICES.txt" ;;
	manifest-hash) printf 'changed manifest\n' >>"${payload}/GoNotices/manifest.json" ;;
	empty-notices)
		: >"${payload}/GoNotices/THIRD_PARTY_NOTICES.txt"
		write_binding
		;;
	empty-manifest)
		: >"${payload}/GoNotices/manifest.json"
		write_binding
		;;
	empty-binary) : >"${payload}/ksail" ;;
	hash-read-failure)
		sed 's/^notices_sha256=.*/notices_sha256=/' "${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	extra-line) printf 'extra\n' >>"${payload}/GoNotices/release.txt" ;;
	missing-line)
		sed '$d' "${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	duplicate-line)
		binding_duplicate="$(sed -n '2p' "${payload}/GoNotices/release.txt")"
		printf '%s\n' "${binding_duplicate}" >>"${payload}/GoNotices/release.txt"
		;;
	swapped-lines)
		awk 'NR == 2 { second = $0; next } NR == 3 { print; print second; next } { print }' \
			"${payload}/GoNotices/release.txt" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	missing-final-lf)
		printf '%s' "$(cat "${payload}/GoNotices/release.txt")" >"${work}/binding"
		mv "${work}/binding" "${payload}/GoNotices/release.txt"
		;;
	duplicate-binary) duplicate_member=ksail ;;
	duplicate-notices) duplicate_member=GoNotices/THIRD_PARTY_NOTICES.txt ;;
	duplicate-manifest) duplicate_member=GoNotices/manifest.json ;;
	duplicate-binding) duplicate_member=GoNotices/release.txt ;;
	symlink-*)
		case "${test_case}" in
		symlink-binary) duplicate_member=ksail ;;
		symlink-notices) duplicate_member=GoNotices/THIRD_PARTY_NOTICES.txt ;;
		symlink-manifest) duplicate_member=GoNotices/manifest.json ;;
		symlink-binding) duplicate_member=GoNotices/release.txt ;;
		esac
		mv "${payload}/${duplicate_member}" "${work}/linked"
		ln -s "${work}/linked" "${payload}/${duplicate_member}"
		;;
	hardlink-manifest)
		rm "${payload}/GoNotices/manifest.json"
		ln "${payload}/GoNotices/THIRD_PARTY_NOTICES.txt" "${payload}/GoNotices/manifest.json"
		write_binding
		;;
	directory-manifest)
		rm "${payload}/GoNotices/manifest.json"
		mkdir "${payload}/GoNotices/manifest.json"
		;;
	reused-directory)
		mkdir "${notice_dir}"
		printf 'do not replace\n' >"${notice_dir}/retained"
		;;
	unwritable-prefix) chmod 0555 "${install_dir}" ;;
	notices-root-symlink)
		mv "${install_dir}/.ksail-notices" "${work}/notice-outside"
		ln -s "${work}/notice-outside" "${install_dir}/.ksail-notices"
		;;
	binary-directory)
		rm "${install_dir}/ksail"
		mkdir "${install_dir}/ksail"
		;;
	unsupported-architecture) test_arch=s390x ;;
	malformed-version) requested_version='v7.180.5/../../malicious' ;;
	esac
	make_archive
	if [[ "${test_case}" == archive-checksum ]]; then
		printf '%064d  %s\n' 0 "${asset}" >"${fixtures}/${checksums}"
	fi
	make_stubs
	case "${test_case}" in
	race-empty | race-conflict | race-symlink | race-early-directory)
		if run_installer >"${work}/output" 2>&1; then fail 'concurrent notice destination was accepted'; fi
		! grep -Fq 'Installed KSail' "${work}/output" || fail 'concurrent conflict emitted success'
		[[ -s "${work}/race-id" ]] || fail 'concurrent destination control did not execute'
		[[ "$(ls -din "${notice_dir}")" == "$(cat "${work}/race-id")" ]] ||
			fail 'concurrent notice directory identity changed'
		[[ "$(cat "${install_dir}/ksail")" == 'previous binary' ]] || fail 'conflict replaced previous binary'
		case "${test_case}" in
		race-empty) [[ -z "$(find "${notice_dir}" -mindepth 1 -print)" ]] || fail 'concurrent empty directory mutated' ;;
		race-symlink)
			[[ -L "${notice_dir}" && "$(cat "${notice_dir}-outside/retained")" == concurrent ]] ||
				fail 'concurrent linked destination mutated'
			[[ "$(find "${notice_dir}-outside" -mindepth 1 -print)" == "${notice_dir}-outside/retained" ]] ||
				fail 'notice publication traversed a concurrent link'
			;;
		*) [[ "$(find "${notice_dir}" -mindepth 1 -print)" == "${notice_dir}/retained" ]] ||
			fail 'concurrent conflicting directory mutated' ;;
		esac
		;;
	signal-after-reservation-*)
		if run_installer >"${work}/output" 2>&1; then fail 'reservation signal did not interrupt installer'; fi
		! grep -Fq 'Installed KSail' "${work}/output" || fail 'interrupted reservation emitted success'
		[[ "$(cat "${install_dir}/ksail")" == 'previous binary' ]] || fail 'reservation changed binary'
		fault=''
		run_installer >"${work}/retry-output" 2>&1 || fail 'interrupted reservation blocks safe retry'
		for duplicate_member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
			cmp "${payload}/GoNotices/${duplicate_member}" "${notice_dir}/${duplicate_member}" ||
				fail 'reservation retry lost expected notices'
		done
		;;
	identical-reinstall | retained-conflict | retained-symlink | retained-hardlink | \
		retained-extra | retained-directory-link | retained-unreadable | reuse-copy-binary)
		fault=''
		run_installer >"${work}/first-output" 2>&1 || fail 'first install refused'
		case "${test_case}" in
		identical-reinstall)
			chmod 0555 "${notice_dir}"
			chmod 0444 "${notice_dir}/"*
			;;
		retained-conflict) printf 'changed\n' >>"${notice_dir}/manifest.json" ;;
		retained-symlink)
			mv "${notice_dir}/manifest.json" "${work}/retained-manifest"
			ln -s "${work}/retained-manifest" "${notice_dir}/manifest.json"
			;;
		retained-hardlink) ln "${notice_dir}/manifest.json" "${work}/retained-manifest" ;;
		retained-extra) printf 'unexpected\n' >"${notice_dir}/.unexpected" ;;
		retained-unreadable)
			chmod 0111 "${notice_dir}"
			[[ ! -r "${notice_dir}" ]] || fail 'unreadable directory control requires a non-root test user'
			;;
		retained-directory-link)
			mv "${notice_dir}" "${work}/retained-notices"
			ln -s "${work}/retained-notices" "${notice_dir}"
			;;
		reuse-copy-binary) fault=copy-binary ;;
		esac
		retained_identity="$(ls -din "${notice_dir}" "${notice_dir}/THIRD_PARTY_NOTICES.txt" \
			"${notice_dir}/manifest.json" "${notice_dir}/release.txt")"
		binary_identity="$(ls -din "${install_dir}/ksail")"
		retained_bytes="$(cksum "${notice_dir}/THIRD_PARTY_NOTICES.txt" \
			"${notice_dir}/manifest.json" "${notice_dir}/release.txt")"
		if [[ "${test_case}" == identical-reinstall ]]; then
			run_installer >"${work}/output" 2>&1 || fail 'identical same-version reinstall refused'
			grep -Fq 'Installed KSail' "${work}/output" || fail 'identical reinstall did not succeed'
		else
			if run_installer >"${work}/output" 2>&1; then fail 'unsafe retained bundle accepted'; fi
			! grep -Fq 'Installed KSail' "${work}/output" || fail 'failed reuse emitted success'
			[[ "$(ls -din "${install_dir}/ksail")" == "${binary_identity}" ]] || fail 'failed reuse replaced binary'
		fi
		[[ "$(ls -din "${notice_dir}" "${notice_dir}/THIRD_PARTY_NOTICES.txt" \
			"${notice_dir}/manifest.json" "${notice_dir}/release.txt")" == "${retained_identity}" ]] ||
			fail 'existing retained notice identities changed'
		[[ "$(cksum "${notice_dir}/THIRD_PARTY_NOTICES.txt" \
			"${notice_dir}/manifest.json" "${notice_dir}/release.txt")" == "${retained_bytes}" ]] ||
			fail 'existing retained notice bytes changed'
		;;
	signal-after-rename-*)
		if run_installer >"${work}/output" 2>&1; then fail 'interrupted replacement reported success'; fi
		! grep -Fq 'Installed KSail' "${work}/output" || fail 'interrupted replacement emitted success'
		[[ "$("${install_dir}/ksail")" == 'ksail test binary' ]] || fail 'control did not reach real rename'
		for duplicate_member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
			cmp "${payload}/GoNotices/${duplicate_member}" "${notice_dir}/${duplicate_member}" ||
				fail 'interrupted post-commit replacement lost its notice bundle'
		done
		;;
	valid | latest)
		run_installer >"${work}/output" 2>&1 || {
			cat "${work}/output"
			fail "${test_case}: valid release refused"
		}
		[[ -x "${install_dir}/ksail" && "$("${install_dir}/ksail")" == 'ksail test binary' ]] ||
			fail 'installed binary differs'
		for duplicate_member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
			cmp "${payload}/GoNotices/${duplicate_member}" "${notice_dir}/${duplicate_member}" ||
				fail 'retained notice bytes differ'
		done
		grep -Fq "${notice_dir}" "${work}/output" || fail 'retained notice path not reported'
		[[ -z "$(find "${work}/temporary" -mindepth 1 -print)" ]] || fail 'download temp was retained'
		[[ "$(cat "${old_notice}/retained")" == 'previous notices' ]] || fail 'older notices changed'
		;;
	*)
		if run_installer >"${work}/output" 2>&1; then fail "${test_case}: unsafe or incomplete installation accepted"; fi
		! grep -Fq 'Installed KSail' "${work}/output" || fail "${test_case}: false success"
		if [[ "${test_case}" == missing-notices ]]; then
			grep -Fq 'GoNotices/THIRD_PARTY_NOTICES.txt' "${work}/output" ||
				fail 'old release refusal does not identify required GoNotices member'
		fi
		if [[ "${test_case}" != binary-directory ]]; then
			[[ "$(cat "${install_dir}/ksail")" == 'previous binary' ]] || fail "${test_case}: previous binary changed"
		fi
		[[ "$(cat "${old_notice}/retained")" == 'previous notices' ]] || fail "${test_case}: previous notices changed"
		if [[ "${test_case}" == reused-directory ]]; then
			[[ "$(cat "${notice_dir}/retained")" == 'do not replace' ]] || fail 'existing version directory changed'
		elif [[ "${test_case}" == final-rename ]]; then
			for duplicate_member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
				cmp "${payload}/GoNotices/${duplicate_member}" "${notice_dir}/${duplicate_member}" ||
					fail 'attempted binary commit lost validated notices'
			done
		else
			[[ ! -e "${notice_dir}" ]] || fail "${test_case}: failed installation left version directory"
		fi
		;;
	esac
	[[ "$(cat "${old_notice}/retained")" == 'previous notices' ]] || fail 'older notices changed'
	[[ -z "$(find "${work}/temporary" -mindepth 1 -print)" ]] || fail "${test_case}: download temporary files remain"
	[[ -z "$(find "${install_dir}" -maxdepth 1 -name '.ksail-install.*' -print)" ]] ||
		fail "${test_case}: staged binary remains"
	[[ -z "$(find "${install_dir}/.ksail-notices" -maxdepth 1 -name '.ksail-notices-stage.*' -print)" ]] ||
		fail "${test_case}: private notice staging remains"
}

if [[ "${1:-}" == --case ]]; then
	run_case "$2"
	exit 0
fi

failures=0
for test_case in valid latest identical-reinstall empty-notices empty-manifest empty-binary \
	signal-after-reservation-HUP signal-after-reservation-INT signal-after-reservation-TERM \
	signal-after-rename-HUP signal-after-rename-INT \
	signal-after-rename-TERM missing-notices missing-manifest missing-binding stale-version wrong-target \
	retained-conflict retained-symlink retained-hardlink retained-extra retained-directory-link reuse-copy-binary \
	retained-unreadable \
	race-empty race-conflict race-symlink race-early-directory \
	zero-source malformed-source notices-hash manifest-hash hash-read-failure extra-line missing-line duplicate-line \
	swapped-lines \
	missing-final-lf duplicate-binary duplicate-notices duplicate-manifest duplicate-binding traversal absolute \
	symlink-binary symlink-notices symlink-manifest symlink-binding hardlink-manifest directory-manifest \
	copy-binary copy-notices final-rename reused-directory unwritable-prefix notices-root-symlink binary-directory \
	archive-checksum unsupported-architecture malformed-version; do
	if "${BASH}" "$0" --case "${test_case}"; then
		printf 'PASS: %s\n' "${test_case}"
	else
		failures=$((failures + 1))
	fi
done
[[ "${failures}" == 0 ]] || fail "${failures} installer cases failed"

release_workflow="${repo_root}/.github/workflows/cd.yaml"
for release_guard in 'const requiredInstallerAssets = [' "'install.sh'," 'missing required installer release assets'; do
	grep -Fq "${release_guard}" "${release_workflow}" || fail "release workflow does not enforce ${release_guard}"
done
printf 'All release installer cases passed.\n'
