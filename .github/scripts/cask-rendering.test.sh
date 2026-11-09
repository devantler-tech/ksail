#!/usr/bin/env bash
# Exercise the real GoReleaser -> brew style -> all-platform cask loader boundary (#7943).
# Only the build payload is replaced with a tiny fixture; cask configuration comes from this repo.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for tool in go goreleaser yq brew jq git tar unzip cmp; do
	command -v "$tool" >/dev/null || {
		printf 'missing required tool: %s\n' "$tool" >&2
		exit 1
	}
done

export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1
work="$(mktemp -d)"
# Homebrew 7 requires trust before readall will inspect a non-official tap. Isolate this test's
# trust store so a skipped, untrusted tap cannot report success or alter the user's trust list.
export XDG_CONFIG_HOME="$work/config"
tap="agentic-validation/cask-render-$$-${RANDOM}"
tap_added=false
completed=false
# cleanup retires the temporary tap and fixtures while preserving any failure status.
cleanup() {
	result=$?
	if ! "$completed" && [[ "$result" -eq 0 ]]; then result=1; fi
	if "$tap_added"; then
		brew untap --force "$tap" || result=1
	fi
	rm -rf "$work"
	exit "$result"
}
trap cleanup EXIT

stage_notice_fixtures() {
	local fixture="$1" target member
	for target in cli-darwin-arm64 cli-linux-amd64 cli-linux-arm64 cli-windows-amd64 cli-windows-arm64 desktop-darwin-arm64; do
		mkdir -p "$fixture/.cache/go-notices/$target/GoNotices"
		for member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
			printf 'cask notice placement fixture: %s %s\n' "$target" "$member" >"$fixture/.cache/go-notices/$target/GoNotices/$member"
		done
	done
}

verify_notice_fixture_archives() {
	local fixture="$1" product="$2" archive base target prefix member count=0 expected=5
	if [[ "$product" == ksail-desktop ]]; then expected=1; fi
	while IFS= read -r archive; do
		base="${archive##*/}"
		case "$product:$base" in
		ksail:*_darwin_arm64.tar.gz) target=cli-darwin-arm64 ;;
		ksail:*_linux_amd64.tar.gz) target=cli-linux-amd64 ;;
		ksail:*_linux_arm64.tar.gz) target=cli-linux-arm64 ;;
		ksail:*_windows_amd64.zip) target=cli-windows-amd64 ;;
		ksail:*_windows_arm64.zip) target=cli-windows-arm64 ;;
		ksail-desktop:*_darwin_arm64.zip) target=desktop-darwin-arm64 ;;
		*)
			printf 'unexpected cask fixture archive\n' >&2
			return 1
			;;
		esac
		prefix=GoNotices
		if [[ "$product" == ksail-desktop ]]; then prefix=KSail.app/Contents/Resources/GoNotices; fi
		for member in THIRD_PARTY_NOTICES.txt manifest.json release.txt; do
			case "$archive" in
			*.tar.gz) tar -xOzf "$archive" "$prefix/$member" >"$fixture/member.actual" ;;
			*.zip) unzip -p "$archive" "$prefix/$member" >"$fixture/member.actual" ;;
			esac
			cmp "$fixture/.cache/go-notices/$target/GoNotices/$member" "$fixture/member.actual"
		done
		if [[ "$product" == ksail ]]; then
			for member in bash zsh fish; do
				case "$archive" in
				*.tar.gz) tar -xOzf "$archive" "completions/ksail.$member" >"$fixture/member.actual" ;;
				*.zip) unzip -p "$archive" "completions/ksail.$member" >"$fixture/member.actual" ;;
				esac
				printf '# completion archive fixture: %s\n' "$member" | cmp - "$fixture/member.actual"
			done
		fi
		if [[ "$product" == ksail-desktop ]]; then
			unzip -p "$archive" KSail.app/Contents/Resources/LICENSE >"$fixture/member.actual"
			cmp "$fixture/LICENSE" "$fixture/member.actual"
		fi
		count=$((count + 1))
	done < <(find "$fixture/dist" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \))
	[[ "$count" -eq "$expected" ]]
}

mkdir -p "$work/tap/Casks"
for name in ksail ksail-desktop; do
	config=.goreleaser.yaml
	if [[ "$name" == ksail-desktop ]]; then config=.goreleaser.desktop.yaml; fi
	fixture="$work/$name"
	mkdir -p "$fixture"
	printf 'module cask-render-fixture\n\ngo 1.27.3\n' >"$fixture/go.mod"
	printf 'package main\nfunc main() {}\n' >"$fixture/main.go"
	if [[ "$name" == ksail ]]; then
		cat >"$fixture/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "completion" {
		os.Exit(2)
	}
	fmt.Printf("# completion archive fixture: %s\n", os.Args[2])
}
EOF
		mkdir -p "$fixture/.github/scripts"
		cp "$root/.github/scripts/generate-completions.sh" "$fixture/.github/scripts/"
		for mode in failed empty; do
			cat >"$fixture/completion-$mode" <<'EOF'
#!/usr/bin/env bash
case "${0##*/}:$2" in
completion-failed:fish) exit 9 ;;
completion-empty:fish) exit 0 ;;
esac
printf 'completion fixture\n'
EOF
			chmod +x "$fixture/completion-$mode"
			if bash "$fixture/.github/scripts/generate-completions.sh" "$fixture/completion-$mode" \
				"$fixture/incomplete-$mode" darwin/arm64 darwin/arm64; then
				printf 'the %s completion generation was accepted\n' "$mode" >&2
				exit 1
			fi
			[[ ! -e "$fixture/incomplete-$mode" ]]
		done
	fi
	cp "$root/LICENSE" "$fixture/LICENSE"
	stage_notice_fixtures "$fixture"
	# Keep the real archive/target/cask shapes. Remove application-only build hooks and publication
	# credentials, and force upload off; this fixture must never publish or use a credential.
	yq '.project_name = "'"$name"'" | .dist = "dist" |
		.before.hooks = [] | .builds[].main = "." | .builds[].tags = [] |
		.builds[].env = ["CGO_ENABLED=0"] | del(.builds[].hooks.pre) | .builds[].ldflags = [] |
		.archives[].files = ((.archives[].files // []) | map(select((.src // "") == "LICENSE" or ((.src // "") | test("^\\.cache/go-notices/|^dist/completions/"))))) |
		.homebrew_casks[].skip_upload = "true" |
		.homebrew_casks[].repository.token = "" |
		del(.homebrew_casks[].commit_author.signing, .dockers_v2, .docker_signs, .release.extra_files)' \
		"$root/$config" >"$fixture/.goreleaser.yaml"
	if [[ "$name" == ksail-desktop ]]; then
		yq -i '.builds[].hooks = {}' "$fixture/.goreleaser.yaml"
	fi
	if [[ "$name" == ksail && "$(go env GOARCH)" == amd64 ]]; then
		# Intel still exercises the real script and cask. Its native fixture is
		# built separately because the release matrix excludes Darwin amd64.
		go build -o "$fixture/native-fixture" "$fixture/main.go"
		yq -i '.builds[].hooks.post[0].cmd |= sub("\\{\\{ \\.Path \\}\\}", "{{ .Env.KSAIL_CASK_NATIVE_FIXTURE }}") |
			.builds[].hooks.post[0].cmd |= sub("\\{\\{ \\.Runtime.Goos \\}\\}/\\{\\{ \\.Runtime.Goarch \\}\\}", "darwin/arm64")' "$fixture/.goreleaser.yaml"
	fi
	git -C "$fixture" init --quiet
	git -C "$fixture" remote add origin https://github.com/devantler-tech/ksail.git
	git -C "$fixture" add go.mod main.go .goreleaser.yaml
	git -C "$fixture" -c user.name=fixture -c user.email=fixture@example.invalid \
		-c commit.gpgsign=false commit --quiet -m fixture
	git -C "$fixture" tag v7.204.0
	(
		cd "$fixture"
		export KSAIL_CASK_NATIVE_FIXTURE="$fixture/native-fixture"
		goreleaser release --snapshot --clean --skip=validate,publish,announce,sign
	)
	verify_notice_fixture_archives "$fixture" "$name"
	casks=()
	while IFS= read -r file; do casks+=("$file"); done < <(find "$fixture/dist" -name "$name.rb" -type f)
	[[ ${#casks[@]} -eq 1 ]] || {
		printf 'expected one generated %s cask\n' "$name" >&2
		exit 1
	}
	# Judge the cask as GoReleaser renders it, before any autocorrect (#8196). Homebrew refuses a
	# cask that combines `depends_on :macos` with a versioned macOS dependency, and `brew style
	# --fix` hid that by deleting one of the two. So allow at most one macOS dependency, and fail
	# when the style pass adds, drops or rewrites any dependency.
	macos_dependencies="$(grep -cE '^[[:space:]]*depends_on[[:space:]]+(:macos|macos:)' "${casks[0]}" || true)"
	[[ "$macos_dependencies" -le 1 ]] || {
		printf '%s cask declares %s macOS dependencies; Homebrew accepts only one\n' \
			"$name" "$macos_dependencies" >&2
		exit 1
	}
	dependencies_before="$(sed -nE 's/^[[:space:]]*(depends_on[[:space:]].*)$/\1/p' "${casks[0]}" | LC_ALL=C sort)"
	brew style --fix "${casks[0]}"
	dependencies_after="$(sed -nE 's/^[[:space:]]*(depends_on[[:space:]].*)$/\1/p' "${casks[0]}" | LC_ALL=C sort)"
	[[ "$dependencies_before" == "$dependencies_after" ]] || {
		printf 'brew style --fix changed the %s cask dependencies:\nbefore:\n%s\nafter:\n%s\n' \
			"$name" "$dependencies_before" "$dependencies_after" >&2
		exit 1
	}
	cp "${casks[0]}" "$work/tap/Casks/$name.rb"
done

git -C "$work/tap" init --quiet
git -C "$work/tap" add Casks/ksail.rb Casks/ksail-desktop.rb
git -C "$work/tap" -c user.name=fixture -c user.email=fixture@example.invalid \
	-c commit.gpgsign=false commit --quiet -m fixture
brew tap --custom-remote "$tap" "$work/tap"
tap_added=true
brew trust "$tap"
brew readall --os=all --arch=all "$tap"

# This runner is macOS. Both casks must retain a real checksum and the architecture
# requirement after rendering and style cleanup; no product binary is installed or executed.
for name in ksail ksail-desktop; do
	brew info --cask --json=v2 "$tap/$name" >"$work/$name.json"
	jq -e '.casks | length == 1' "$work/$name.json" >/dev/null
	jq -e '.casks[0].sha256 | test("^[0-9a-f]{64}$")' "$work/$name.json" >/dev/null
	jq -e '.casks[0].depends_on.arch | any(.type == "arm" and .bits == 64)' "$work/$name.json" >/dev/null
	# The desktop cask is macOS-only, so it must still declare its macOS dependency once loaded.
	if [[ "$name" == ksail-desktop ]]; then
		jq -e '.casks[0].depends_on | has("macos")' "$work/$name.json" >/dev/null
	fi
done
jq -e '.casks[0].artifacts as $a |
	($a | any(.bash_completion?[0] == "completions/ksail.bash")) and
	($a | any(.zsh_completion?[0] == "completions/ksail.zsh")) and
	($a | any(.fish_completion?[0] == "completions/ksail.fish")) and
	($a | all(has("generate_completions_from_executable") | not))' "$work/ksail.json" >/dev/null
printf 'PASS: both rendered casks load on every platform and preserve checksum, architecture and macOS guards\n'
completed=true
