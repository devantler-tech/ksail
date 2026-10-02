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
printf 'otelzap provenance negative controls passed\n'
