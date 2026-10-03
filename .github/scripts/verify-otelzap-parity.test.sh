#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT
mkdir "${temporary}/upstream"
printf 'module example.invalid/fixture\n' >"${temporary}/upstream/go.mod"
printf 'fixture license\n' >"${temporary}/upstream/LICENSE"
printf 'package otelzap\n' >"${temporary}/upstream/conv.go"
cat >"${temporary}/change.patch" <<'PATCH'
--- a/conv.go
+++ b/conv.go
@@ -1 +1,2 @@
 package otelzap
+const compatibility = true
PATCH
cp -R "${temporary}/upstream" "${temporary}/local"
patch -d "${temporary}/local" -p1 <"${temporary}/change.patch" >/dev/null

verify() {
	bash "${script_dir}/verify-otelzap-parity.sh" \
		--upstream-dir "${temporary}/upstream" --local-dir "$1" \
		--patch-file "${temporary}/change.patch" >"${temporary}/result" 2>&1
}
verify "${temporary}/local"

for mutation in extra nested-note changed missing source-link root-link root-link-slash root-link-dot; do
	cp -R "${temporary}/local" "${temporary}/${mutation}"
	candidate="${temporary}/${mutation}"
	case "${mutation}" in
	extra) printf 'package otelzap\n' >"${candidate}/extra.go" ;;
	nested-note)
		mkdir "${candidate}/nested"
		printf 'undeclared\n' >"${candidate}/nested/KSail-PATCH.md"
		;;
	changed) printf '// unaudited change\n' >>"${candidate}/conv.go" ;;
	missing) rm "${candidate}/LICENSE" ;;
	source-link)
		rm "${candidate}/conv.go"
		ln -s "${temporary}/local/conv.go" "${candidate}/conv.go"
		;;
	root-link | root-link-slash | root-link-dot)
		ln -s "${candidate}" "${candidate}-link"
		candidate="${candidate}-link"
		case "${mutation}" in
		root-link-slash) candidate="${candidate}/" ;;
		root-link-dot) candidate="${candidate}/." ;;
		esac
		;;
	esac
	if verify "${candidate}"; then
		printf 'provenance guard accepted %s\n' "${mutation}" >&2
		exit 1
	fi
done

# Retain valid cached checksum metadata while changing the extracted source and
# adapter together. Byte parity alone must not authenticate this undeclared edit.
repo_root="$(cd -- "${script_dir}/../.." && pwd -P)"
real_go="$(command -v go)"
resolved="$(go mod download -json github.com/uptrace/opentelemetry-go-extra/otelzap@v0.3.2)"
cp -R "$(jq -er '.Dir' <<<"${resolved}")" "${temporary}/cached"
cp -R "${repo_root}/third_party/otelzap" "${temporary}/adapter"
chmod -R u+w "${temporary}/cached" "${temporary}/adapter"
jq --arg dir "${temporary}/cached" '.Dir = $dir' <<<"${resolved}" >"${temporary}/metadata.json"
mkdir "${temporary}/bin"
cat >"${temporary}/bin/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
if [[ $# -eq 4 && "$1" == mod && "$2" == download && "$3" == -json &&
	"$4" == github.com/uptrace/opentelemetry-go-extra/otelzap@v0.3.2 ]]; then
	cat "${KS_SOURCE_METADATA}"
else
	exec "${KS_SOURCE_GO}" "$@"
fi
GO
chmod +x "${temporary}/bin/go"

verify_cached() {
	PATH="${temporary}/bin:${PATH}" KS_SOURCE_METADATA="${temporary}/metadata.json" \
		KS_SOURCE_GO="${real_go}" bash "${script_dir}/verify-otelzap-parity.sh" \
		--local-dir "${temporary}/adapter" >"${temporary}/cached-result" 2>&1
}
verify_cached
printf '// undeclared cached source edit\n' >>"${temporary}/cached/global.go"
printf '// undeclared cached source edit\n' >>"${temporary}/adapter/global.go"
if verify_cached; then
	printf 'provenance guard accepted changed source with unchanged cached checksum metadata\n' >&2
	exit 1
fi
grep -q 'module source checksum mismatch' "${temporary}/cached-result" || {
	cat "${temporary}/cached-result" >&2
	exit 1
}
printf 'otelzap provenance negative controls passed\n'
