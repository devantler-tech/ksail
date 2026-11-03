#!/usr/bin/env bash
# Exercise the real GoReleaser -> brew style -> all-platform cask loader boundary (#7943).
# Only the build payload is replaced with a tiny fixture; cask configuration comes from this repo.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for tool in go goreleaser yq brew jq git; do
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

mkdir -p "$work/tap/Casks"
for name in ksail ksail-desktop; do
	config=.goreleaser.yaml
	if [[ "$name" == ksail-desktop ]]; then config=.goreleaser.desktop.yaml; fi
	fixture="$work/$name"
	mkdir -p "$fixture"
	printf 'module cask-render-fixture\n\ngo 1.27.3\n' >"$fixture/go.mod"
	printf 'package main\nfunc main() {}\n' >"$fixture/main.go"
	# Keep the real archive/target/cask shapes. Remove application-only build hooks and publication
	# credentials, and force upload off; this fixture must never publish or use a credential.
	yq '.project_name = "'"$name"'" | .dist = "dist" |
		.before.hooks = [] | .builds[].main = "." | .builds[].tags = [] |
		.builds[].env = ["CGO_ENABLED=0"] | .builds[].hooks = {} | .builds[].ldflags = [] |
		.archives[].files = [] | .homebrew_casks[].skip_upload = "true" |
		.homebrew_casks[].repository.token = "" |
		del(.homebrew_casks[].commit_author.signing, .dockers_v2, .docker_signs, .release.extra_files)' \
		"$root/$config" >"$fixture/.goreleaser.yaml"
	git -C "$fixture" init --quiet
	git -C "$fixture" remote add origin https://github.com/devantler-tech/ksail.git
	git -C "$fixture" add go.mod main.go .goreleaser.yaml
	git -C "$fixture" -c user.name=fixture -c user.email=fixture@example.invalid \
		-c commit.gpgsign=false commit --quiet -m fixture
	git -C "$fixture" tag v7.204.0
	(
		cd "$fixture"
		goreleaser release --snapshot --clean --skip=validate,publish,announce,sign
	)
	casks=()
	while IFS= read -r file; do casks+=("$file"); done < <(find "$fixture/dist" -name "$name.rb" -type f)
	[[ ${#casks[@]} -eq 1 ]] || {
		printf 'expected one generated %s cask\n' "$name" >&2
		exit 1
	}
	brew style --fix "${casks[0]}"
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
done
printf 'PASS: both rendered casks load on every platform and preserve checksum/architecture guards\n'
completed=true
