#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
umask 077
yq -o=json '.' "$repo_root/.github/workflows/cd.yaml" >"$work/workflow.json"
version=v1.2.3
source_sha=1111111111111111111111111111111111111111

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

extract() {
	jq -er --arg job "$1" --arg id "$2" \
		'.jobs[$job].steps[] | select(.id == $id) | .run | select(type == "string" and length > 0)' \
		"$work/workflow.json" >"$3" || fail "missing executable $1/$2"
}

contracts() {
	yq -o=json '.' "$repo_root/.goreleaser.yaml" | jq -e '
		.before.hooks | index("go run .github/scripts/go-notices.go stage-cli {{ .Tag }} {{ .FullCommit }}") != null
	' >/dev/null || fail 'CLI source-bound before hook missing'
	yq -o=json '.' "$repo_root/.goreleaser.yaml" | jq -e '
		.archives[0].files as $f |
		($f | index("LICENSE*") != null) and ($f | index("README*") != null) and
		($f | any(type == "object" and .src == ".cache/go-notices/cli-{{ .Os }}-{{ .Arch }}/GoNotices/*"
		  and .dst == "GoNotices" and .strip_parent == true))
	' >/dev/null || fail 'CLI archives omit notices or original default documents'
	yq -o=json '.' "$repo_root/.goreleaser.desktop.yaml" | jq -e '
		(.before.hooks | index("go run .github/scripts/go-notices.go stage desktop-darwin-arm64 \"{{ .Tag }}\" \"{{ .FullCommit }}\"") != null) and
		(.archives[0].files as $files |
		  all(["THIRD_PARTY_NOTICES.txt", "manifest.json", "release.txt"][]; . as $name |
		    $files | any(.src == (".cache/go-notices/desktop-darwin-arm64/GoNotices/" + $name) and
		      .dst == ("KSail.app/Contents/Resources/GoNotices/" + $name))) and
		  ($files | any(.src == "LICENSE" and .dst == "KSail.app/Contents/Resources/LICENSE")))
	' >/dev/null || fail 'macOS app bundle omits staged notices or project LICENSE'
	jq -e '
		def idx($j; $id): .jobs[$j].steps | map(.id) | index($id);
		def named($j; $name): .jobs[$j].steps | map(.name) | index($name);
		def before($a; $b): ($a | type) == "number" and ($b | type) == "number" and $a < $b;
		before(named("goreleaser"; "🏗️ Run GoReleaser"); idx("goreleaser"; "verify_cli_notices")) and
		before(idx("desktop-macos"; "verify_macos_notices"); named("desktop-macos"; "📤 Upload desktop archive artifact")) and
		before(idx("desktop"; "package"); idx("desktop"; "verify_desktop_notices")) and
		before(idx("desktop"; "verify_desktop_notices"); named("desktop"; "📤 Upload desktop archive artifact")) and
		([.jobs.goreleaser.steps[], .jobs["desktop-macos"].steps[] |
		  select(.id == "verify_cli_notices" or .id == "verify_macos_notices") | .if] ==
		  ["needs.cask-checkpoint.outputs.release-mode == '\''build'\''", "needs.cask-checkpoint.outputs.release-mode == '\''build'\''"]) and
		([.jobs.desktop.steps[] | select(.id == "verify_desktop_notices") | .if] ==
		  ["needs.validate-release-ref.outputs.release-mode == '\''build'\''"]) and
		([.jobs[] .steps[] | select(.id // "" | test("^verify_.*_notices$")) |
		  .["continue-on-error"] // false] | all(. == false))
	' "$work/workflow.json" >/dev/null || fail 'release verification order/admission changed'
	printf 'PASS: release configuration and mandatory ordering\n'
}

make_stubs() {
	mkdir "$work/bin"
	cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1 $2" == 'run .github/scripts/go-notices.go' ]] || exit 91
shift 2
op="$1"; shift
printf '%s\n' "$op $*" >>"$FIXTURE/commands"
case "$op" in
stage)
	[[ "$2" == v1.2.3 && "$3" == 1111111111111111111111111111111111111111 ]] || exit 92
	[[ "${FAULT:-}" != stage ]] || exit 93
	dir=".cache/go-notices/$1/GoNotices"
	mkdir -p "$dir"
	for name in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do printf '%s\n' "$name" >"$dir/$name"; done
	[[ "${FAULT:-}" != missing-stage ]] || rm "$dir/manifest.json"
	;;
verify-cli)
	[[ "$1" == v1.2.3 && "$2" == 1111111111111111111111111111111111111111 ]] || exit 92
	[[ "${FAULT:-}" != verify ]] || exit 94
	;;
verify)
	[[ "$2" == v1.2.3 && "$3" == 1111111111111111111111111111111111111111 ]] || exit 92
	[[ "${FAULT:-}" != verify ]] || exit 94
	case "$1" in
	desktop-darwin-arm64) exit 0 ;;
	desktop-linux-amd64) archive=ksail-desktop_1.2.3_linux_amd64.tar.gz; tar -tzf "$archive" >"$FIXTURE/members" ;;
	desktop-windows-amd64) archive=ksail-desktop_1.2.3_windows_amd64.zip; unzip -Z1 "$archive" >"$FIXTURE/members" ;;
	*) exit 97 ;;
	esac
	for name in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do grep -qx "GoNotices/$name" "$FIXTURE/members"; done
	;;
*) exit 96 ;;
esac
STUB
	# 7z is native on the Windows runner. This adapter checks its exact member arguments and
	# uses the host zip only to inspect this workflow's member selection without installing 7z.
	cat >"$work/bin/7z" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == a ]] || exit 71
archive="$2"; shift 2
[[ ( $# == 2 && "$1" == ./desktop/ksail-desktop.exe && "$2" == LICENSE ) || ( $# == 1 && "$1" == GoNotices ) ]] || exit 72
exec /usr/bin/zip -qr "$archive" "$@"
STUB
	chmod +x "$work/bin/"*
}

producer_tests() {
	local job id operation fault dir
	for job in goreleaser desktop-macos; do
		if [[ "$job" == goreleaser ]]; then
			id=verify_cli_notices
			operation=verify-cli
		else
			id=verify_macos_notices
			operation='verify desktop-darwin-arm64'
		fi
		extract "$job" "$id" "$work/$id.sh"
		for fault in none verify; do
			dir="$work/$id-$fault"
			mkdir "$dir"
			if (cd "$dir" && PATH="$work/bin:$PATH" FIXTURE="$dir" FAULT="$fault" \
				GITHUB_REF_NAME="$version" GITHUB_SHA="$source_sha" "$BASH" -e "$work/$id.sh") >"$dir/log" 2>&1; then
				[[ "$fault" == none ]] || fail "$id ignored verifier failure"
				grep -qx "$operation $version $source_sha" "$dir/commands" || fail "$id omitted source-bound verifier"
			else
				[[ "$fault" != none ]] || fail "$id refused valid helper result"
			fi
		done
	done
	printf 'PASS: CLI/macOS producer source binding and mandatory failure propagation\n'
}

package_tests() {
	extract desktop package "$work/package.sh"
	extract desktop verify_desktop_notices "$work/verify-desktop.sh"
	local os fault dir archive
	for os in linux windows; do
		for fault in none stage missing-stage verify existing-archive symlink-archive; do
			dir="$work/package-$os-$fault"
			mkdir -p "$dir/desktop"
			printf binary >"$dir/desktop/ksail-desktop"
			printf binary >"$dir/desktop/ksail-desktop.exe"
			printf 'project license fixture\n' >"$dir/LICENSE"
			archive="ksail-desktop_1.2.3_${os}_amd64"
			if [[ "$os" == linux ]]; then archive+=.tar.gz; else archive+=.zip; fi
			case "$fault" in
			existing-archive) printf retained >"$dir/$archive" ;;
			symlink-archive) ln -s "$dir/outside" "$dir/$archive" ;;
			esac
			if (cd "$dir" && PATH="$work/bin:$PATH" FIXTURE="$dir" FAULT="$fault" \
				GOOS="$os" ARCH=amd64 VERSION=1.2.3 GITHUB_REF_NAME="$version" GITHUB_SHA="$source_sha" \
				GITHUB_OUTPUT="$dir/output" "$BASH" -e "$work/package.sh" &&
				PATH="$work/bin:$PATH" FIXTURE="$dir" FAULT="$fault" GOOS="$os" ARCH=amd64 \
					GITHUB_REF_NAME="$version" GITHUB_SHA="$source_sha" "$BASH" -e "$work/verify-desktop.sh") >"$dir/log" 2>&1; then
				[[ "$fault" == none ]] || fail "$os accepted $fault"
			else
				[[ "$fault" != none ]] || {
					cat "$dir/log"
					fail "$os packaging"
				}
				continue
			fi
			if [[ "$os" == linux ]]; then tar -tzf "$dir/$archive" >"$dir/members"; else unzip -Z1 "$dir/$archive" >"$dir/members"; fi
			grep -qx 'GoNotices/manifest.json' "$dir/members" || fail "$os missing manifest"
			grep -qx 'GoNotices/THIRD_PARTY_NOTICES.txt' "$dir/members" || fail "$os missing notices"
			grep -qx 'GoNotices/release.txt' "$dir/members" || fail "$os missing binding"
			grep -qx LICENSE "$dir/members" || fail "$os missing project LICENSE"
			if [[ "$os" == linux ]]; then
				tar -xOzf "$dir/$archive" LICENSE >"$dir/license-actual"
			else
				unzip -p "$dir/$archive" LICENSE >"$dir/license-actual"
			fi
			cmp "$dir/LICENSE" "$dir/license-actual" || fail "$os changed project LICENSE bytes"
			if [[ "$os" == linux ]]; then grep -qx ksail-desktop "$dir/members"; else grep -qx desktop/ksail-desktop.exe "$dir/members"; fi
		done
	done
	printf 'PASS: Linux/Windows archive members and stage/verify refusals\n'
}

case "${1:-all}" in
contracts) contracts ;;
package)
	make_stubs
	package_tests
	;;
producers)
	make_stubs
	producer_tests
	;;
all)
	contracts
	make_stubs
	producer_tests
	package_tests
	;;
*) fail 'unknown test selection' ;;
esac
