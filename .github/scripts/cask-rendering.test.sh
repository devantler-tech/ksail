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
	# Packaging uses literal payloads here; the command-package tests exercise the real generator.
	if [[ "$name" == ksail ]]; then
		cat >"$fixture/fixture-completions.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" != linux/amd64 ]]; then exit 0; fi
mkdir -p dist/completions
for shell in bash zsh fish; do
	printf '# completion archive fixture: %s\n' "$shell" >"dist/completions/ksail.$shell"
done
EOF
	fi
	# Keep the real archive/target/cask shapes. Remove application-only build hooks and publication
	# credentials, and force upload off; this fixture must never publish or use a credential.
	yq '.project_name = "'"$name"'" | .dist = "dist" |
		.before.hooks = [] | .builds[].main = "." | .builds[].tags = [] |
		.builds[].env = ["CGO_ENABLED=0"] | .builds[].hooks = {} | .builds[].ldflags = [] |
		.homebrew_casks[].skip_upload = "true" |
		.homebrew_casks[].repository.token = "" |
		del(.homebrew_casks[].commit_author.signing, .dockers_v2, .docker_signs, .release.extra_files)' \
		"$root/$config" >"$fixture/.goreleaser.yaml"
	if [[ "$name" == ksail ]]; then
		yq -i '.builds[].hooks.post = [{"cmd": "bash fixture-completions.sh {{ .Os }}/{{ .Arch }}"}]' \
			"$fixture/.goreleaser.yaml"
	else
		yq -i '.archives[].files = []' "$fixture/.goreleaser.yaml"
	fi
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
	if [[ "$name" == ksail ]]; then
		shopt -s nullglob
		archives=("$fixture"/dist/*.tar.gz "$fixture"/dist/*.zip)
		[[ ${#archives[@]} -eq 5 ]] || {
			printf 'expected all five CLI archives\n' >&2
			exit 1
		}
		for archive in "${archives[@]}"; do
			for shell in bash zsh fish; do
				actual="$(tar -xOf "$archive" "completions/ksail.$shell")"
				[[ "$actual" == "# completion archive fixture: $shell" ]] || {
					printf 'missing or changed %s completion in %s\n' "$shell" "$archive" >&2
					exit 1
				}
			done
		done
	fi
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
