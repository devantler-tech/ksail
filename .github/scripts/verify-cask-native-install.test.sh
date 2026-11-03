#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
subject="$script_dir/verify-cask-native-install.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
real_git="$(command -v git)"
mkdir -p "$work/source/Casks" "$work/bin"
git -C "$work/source" init --quiet
printf 'cask "ksail" do\n  version "7.202.77"\nend\n' >"$work/source/Casks/ksail.rb"
git -C "$work/source" add Casks/ksail.rb
git -C "$work/source" -c core.hooksPath=/dev/null -c user.name=fixture -c user.email=fixture@example.invalid \
	-c commit.gpgsign=false commit --quiet -m fixture
tap_sha="$(git -C "$work/source" rev-parse HEAD)"
blob_sha="$(git -C "$work/source" rev-parse HEAD:Casks/ksail.rb)"
# The remote advances before the trial. The consumer must install the older receipt-bound tree.
printf 'cask "ksail" do\n  version "7.202.78"\nend\n' >"$work/source/Casks/ksail.rb"
git -C "$work/source" add Casks/ksail.rb
git -C "$work/source" -c core.hooksPath=/dev/null -c user.name=fixture -c user.email=fixture@example.invalid \
	-c commit.gpgsign=false commit --quiet -m advance

cat >"$work/bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
args=()
for arg in "$@"; do
  if [[ "$arg" == https://github.com/devantler-tech/homebrew-tap.git ]]; then arg="$FIXTURE_SOURCE"; fi
  args+=("$arg")
done
exec "$REAL_GIT" "${args[@]}"
EOF
cat >"$work/bin/uname" <<'EOF'
#!/usr/bin/env bash
[[ "$1" == -s ]] && echo Darwin || echo arm64
EOF
cat >"$work/bin/probe" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'probe\n' >> "$FIXTURE_STATE/calls"
case "$FIXTURE_MODE" in
  failed-tab) exit 1 ;;
  unobserved-tab) exit 0 ;;
  omitted-tab) echo '{"tabObserved":false,"observedBuffer":"ksail cl"}' ;;
  *) echo '{"tabObserved":true,"observedBuffer":"ksail cluster "}' ;;
esac
EOF
cat >"$work/bin/brew" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'brew %s\n' "$*" >> "$FIXTURE_STATE/calls"
large_tail() {
  local index
  for ((index=0; index<8192; index++)); do
    printf 'unrelated-entry-%08d-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n' "$index"
  done
}
case "${1:-}" in
  --prefix) printf '%s\n' "$FIXTURE_PREFIX" ;;
  --version) echo 'Homebrew fixture' ;;
  --repository) printf '%s\n' "$FIXTURE_STATE/tap" ;;
  trust) : ;;
  tap)
    if [[ $# -eq 1 ]]; then
      if [[ "$FIXTURE_MODE" == preexisting-tap-large || -f "$FIXTURE_STATE/tapped" ]]; then echo devantler-tech/tap; fi
      if [[ "$FIXTURE_MODE" == *-large ]]; then
        echo devantler-tech/tap-other
        large_tail
      fi
    else
      [[ "$2 $3" == '--custom-remote devantler-tech/tap' ]] || exit 99
      "$REAL_GIT" clone --quiet -- "$4" "$FIXTURE_STATE/tap"
      : > "$FIXTURE_STATE/tapped"
    fi ;;
  untap)
    [[ "$FIXTURE_MODE" == cleanup-tap-large ]] || rm -f "$FIXTURE_STATE/tapped"
    rm -rf "$FIXTURE_STATE/tap" ;;
  list)
    if [[ "$FIXTURE_MODE" == preexisting-cask-large || -f "$FIXTURE_STATE/installed" ]]; then echo ksail; fi
    if [[ "$FIXTURE_MODE" == *-large ]]; then
      echo ksail-other
      large_tail
    fi ;;
  info)
    version="$("$REAL_GIT" -C "$FIXTURE_STATE/tap" show HEAD:Casks/ksail.rb | sed -n 's/  version "\([^"]*\)"/\1/p')"
    [[ "$FIXTURE_MODE" != wrong-version ]] || version=7.202.78
    jq -n --arg version "$version" --arg sha "$FIXTURE_SHA" '
      {casks:[{version:$version,sha256:$sha,
        url:("https://github.com/devantler-tech/ksail/releases/download/v"+$version+"/ksail_"+$version+"_darwin_arm64.tar.gz"),
        artifacts:[{bash_completion:["completions/ksail.bash"]},{zsh_completion:["completions/ksail.zsh"]},{fish_completion:["completions/ksail.fish"]}]}]}' ;;
  install)
    : > "$FIXTURE_STATE/installed"
    mkdir -p "$FIXTURE_PREFIX/bin" "$FIXTURE_PREFIX/etc/bash_completion.d" \
      "$FIXTURE_PREFIX/share/zsh/site-functions" "$FIXTURE_PREFIX/share/fish/vendor_completions.d"
    printf '#!/bin/sh\nexit 0\n' > "$FIXTURE_PREFIX/bin/ksail"
    chmod +x "$FIXTURE_PREFIX/bin/ksail"
    echo fixture > "$FIXTURE_PREFIX/etc/bash_completion.d/ksail"
    echo fixture > "$FIXTURE_PREFIX/share/fish/vendor_completions.d/ksail.fish"
    [[ "$FIXTURE_MODE" == missing-zsh ]] || echo fixture > "$FIXTURE_PREFIX/share/zsh/site-functions/_ksail"
    [[ "$FIXTURE_MODE" != failed-install ]] ;;
  uninstall)
    [[ "$FIXTURE_MODE" != failed-cleanup ]] || exit 1
    [[ "$FIXTURE_MODE" == cleanup-cask-large ]] || rm -f "$FIXTURE_STATE/installed"
    rm -f "$FIXTURE_PREFIX/bin/ksail" \
      "$FIXTURE_PREFIX/etc/bash_completion.d/ksail" "$FIXTURE_PREFIX/share/zsh/site-functions/_ksail" \
      "$FIXTURE_PREFIX/share/fish/vendor_completions.d/ksail.fish" ;;
  *) exit 99 ;;
esac
EOF
chmod +x "$work/bin/"*
sha="$(printf 'f%.0s' {1..64})"
modes=("$@")
if [[ "${#modes[@]}" -eq 0 ]]; then
	modes=(valid stale-tag wrong-observer wrong-blob wrong-version missing-zsh failed-install failed-tab unobserved-tab omitted-tab failed-cleanup
		preexisting-cask-large preexisting-tap-large cleanup-cask-large cleanup-tap-large lookalike-large)
fi
for mode in "${modes[@]}"; do
	case_dir="$work/$mode"
	mkdir -p "$case_dir/input" "$case_dir/output" "$case_dir/state" "$case_dir/prefix"
	jq -n --arg tap "$tap_sha" --arg blob "$blob_sha" --arg sha "$sha" '
    {proof_scope:"merged identities, current-main blobs and published asset digests",
     observer_commit:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",release_tag:"v7.202.77",release_id:7,tap_main:$tap,
     casks:[{pr:2019,head:$tap,merge:$tap,path:"Casks/ksail.rb",blob:$blob,
       release_assets:[{name:"ksail_7.202.77_darwin_arm64.tar.gz",digest:("sha256:"+$sha)}]}]}' \
		>"$case_dir/input/receipt.json"
	jq -n --arg sha "$sha" '{id:7,tag_name:"v7.202.77",draft:false,prerelease:false,
    published_at:"2026-10-15T05:15:00Z",assets:[{name:"ksail_7.202.77_darwin_arm64.tar.gz",digest:("sha256:"+$sha)}]}' \
		>"$case_dir/input/release.json"
	case "$mode" in
	stale-tag) jq '.release_tag="v7.202.76"' "$case_dir/input/receipt.json" >"$case_dir/changed" ;;
	wrong-observer) jq '.observer_commit="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "$case_dir/input/receipt.json" >"$case_dir/changed" ;;
	wrong-blob) jq '.casks[0].blob="cccccccccccccccccccccccccccccccccccccccc"' "$case_dir/input/receipt.json" >"$case_dir/changed" ;;
	esac
	if [[ -f "$case_dir/changed" ]]; then mv "$case_dir/changed" "$case_dir/input/receipt.json"; fi
	status=0
	env PATH="$work/bin:$PATH" REAL_GIT="$real_git" FIXTURE_SOURCE="$work/source" \
		FIXTURE_MODE="$mode" FIXTURE_STATE="$case_dir/state" FIXTURE_PREFIX="$case_dir/prefix" FIXTURE_SHA="$sha" \
		RUNNER_TEMP="$case_dir" RECEIPT_DIR="$case_dir/input" NATIVE_EVIDENCE_DIR="$case_dir/output" \
		EXPECTED_TAG=v7.202.77 EXPECTED_CLI_PR=2019 GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
		TAB_PROBE="$work/bin/probe" bash "$subject" >"$case_dir/log" 2>&1 || status=$?
	if [[ "$mode" == valid || "$mode" == lookalike-large ]]; then
		[[ "$status" -eq 0 ]] || {
			cat "$case_dir/log"
			exit 1
		}
		jq -e '.tab_observed == true and .observed_buffer == "ksail cluster " and
      .cask_commit == $tap and .cask_blob == $blob and .release_tag == "v7.202.77" and .cleanup == "complete"' \
			--arg tap "$tap_sha" --arg blob "$blob_sha" "$case_dir/output/receipt.json" >/dev/null
	else
		[[ "$status" -ne 0 && ! -e "$case_dir/output/receipt.json" ]] || {
			printf 'FAIL: %s produced success or a successful receipt\n' "$mode"
			cat "$case_dir/log"
			exit 1
		}
	fi
	case "$mode" in
	preexisting-cask-large | preexisting-tap-large)
		if grep -Eq '^brew (install |tap --custom-remote |untap |uninstall )' "$case_dir/state/calls"; then
			echo 'FAIL: populated runner reached installation or unrelated cleanup'
			exit 1
		fi
		;;
	stale-tag | wrong-observer)
		[[ ! -e "$case_dir/state/calls" ]] || {
			echo 'FAIL: mismatched receipt reached external tools'
			exit 1
		}
		;;
	wrong-blob | wrong-version)
		if grep -q '^brew install ' "$case_dir/state/calls"; then
			echo 'FAIL: unverified cask was installed'
			exit 1
		fi
		;;
	missing-zsh | failed-install)
		if grep -Fxq probe "$case_dir/state/calls"; then
			echo 'FAIL: incomplete install reached Tab observer'
			exit 1
		fi
		;;
	esac
	if [[ "$mode" != failed-cleanup && "$mode" != cleanup-cask-large && "$mode" != cleanup-tap-large ]]; then
		[[ ! -e "$case_dir/state/installed" && ! -e "$case_dir/state/tapped" ]] || exit 1
	fi
done
printf 'PASS: exact receipt, fresh install, Tab failure and owned cleanup controls\n'
