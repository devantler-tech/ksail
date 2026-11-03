#!/usr/bin/env bash
set -euo pipefail
# Only the default-off, disposable native job calls this. Nothing here publishes or merges.
fail() {
	printf 'ERROR: %s\n' "$1" >&2
	exit 1
}
[[ "$(uname -s)/$(uname -m)" == Darwin/arm64 ]] || fail 'Native trial requires macOS arm64'
[[ "${EXPECTED_TAG:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "${EXPECTED_CLI_PR:-}" =~ ^[1-9][0-9]*$ ]] || fail 'Invalid expected release identity'
[[ "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'Missing reviewed observer identity'
mkdir -p "${NATIVE_EVIDENCE_DIR:?}"
rm -f "$NATIVE_EVIDENCE_DIR/receipt.json"
receipt="${RECEIPT_DIR:?}/receipt.json"
release="$RECEIPT_DIR/release.json"
[[ -f "$receipt" && ! -L "$receipt" && -f "$release" && ! -L "$release" ]] || fail 'Missing immutable verification receipt'
jq -e --arg tag "$EXPECTED_TAG" --arg observer "$GITHUB_SHA" --argjson pr "$EXPECTED_CLI_PR" \
	--slurpfile release "$release" '
  def sha: type == "string" and test("^[0-9a-f]{40}$");
  . as $receipt |
  .proof_scope == "merged identities, current-main blobs and published asset digests" and
  .observer_commit == $observer and .release_tag == $tag and (.tap_main | sha) and
  (.release_id | type == "number" and . > 0 and floor == .) and
  .release_id == $release[0].id and $release[0].tag_name == $tag and
  $release[0].draft == false and $release[0].prerelease == false and
  ($release[0].published_at | type == "string" and length > 0) and
  ([.casks[] | select(.path == "Casks/ksail.rb")] | length == 1) and
  ([.casks[] | select(.path == "Casks/ksail.rb")][0] |
    .pr == $pr and (.head | sha) and (.merge | sha) and (.blob | sha) and
    (.release_assets | sort_by(.name)) == ($release[0].assets | map({name,digest}) | sort_by(.name)))
  ' "$receipt" >/dev/null || fail 'Receipt is stale, mismatched or incomplete'
version="${EXPECTED_TAG#v}"
asset="ksail_${version}_darwin_arm64.tar.gz"
archive_sha="$(jq -er --arg asset "$asset" '
  [.assets[] | select(.name == $asset) | .digest] |
  select(length == 1) | .[0] | select(test("^sha256:[0-9a-f]{64}$")) | sub("^sha256:"; "")
  ' "$release")" || fail 'Published native asset digest is missing'
tap_sha="$(jq -r '.tap_main' "$receipt")"
blob_sha="$(jq -r '.casks[] | select(.path == "Casks/ksail.rb") | .blob' "$receipt")"
merge_sha="$(jq -r '.casks[] | select(.path == "Casks/ksail.rb") | .merge' "$receipt")"
work="$(mktemp -d "${RUNNER_TEMP:?}/ksail-native-cask.XXXXXX")"
tap_added=false
install_attempted=false
export XDG_CONFIG_HOME="$work/config" HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1

retire_owned() {
	local status=0 installed taps
	if "$install_attempted"; then
		brew uninstall --cask devantler-tech/tap/ksail >>"$NATIVE_EVIDENCE_DIR/install.log" 2>&1 || status=1
		installed="$(brew list --cask)" || status=1
		if printf '%s\n' "$installed" | grep -Fxq ksail; then status=1; else install_attempted=false; fi
		for path in "$prefix/bin/ksail" "$prefix/etc/bash_completion.d/ksail" \
			"$prefix/share/zsh/site-functions/_ksail" "$prefix/share/fish/vendor_completions.d/ksail.fish"; do
			[[ ! -e "$path" && ! -L "$path" ]] || status=1
		done
	fi
	if "$tap_added"; then
		brew untap --force devantler-tech/tap >>"$NATIVE_EVIDENCE_DIR/install.log" 2>&1 || status=1
		taps="$(brew tap)" || status=1
		if printf '%s\n' "$taps" | grep -Fxq devantler-tech/tap; then status=1; else tap_added=false; fi
	fi
	return "$status"
}
cleanup() {
	local status=$?
	trap - EXIT
	retire_owned || status=1
	rm -rf "$work" || status=1
	if [[ "$status" -ne 0 ]]; then rm -f "$NATIVE_EVIDENCE_DIR/receipt.json"; fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
prefix="$(brew --prefix)"
installed="$(brew list --cask)"
printf '%s\n' "$installed" | grep -Fxq ksail && fail 'A fresh job must not already have the CLI cask'
taps="$(brew tap)"
printf '%s\n' "$taps" | grep -Fxq devantler-tech/tap && fail 'A fresh job must not already have this tap'
for path in "$prefix/bin/ksail" "$prefix/etc/bash_completion.d/ksail" \
	"$prefix/share/zsh/site-functions/_ksail" "$prefix/share/fish/vendor_completions.d/ksail.fish"; do
	[[ ! -e "$path" && ! -L "$path" ]] || fail 'Fresh completion destination is not empty'
done
git clone --quiet --no-checkout -- https://github.com/devantler-tech/homebrew-tap.git "$work/tap"
git -C "$work/tap" checkout --quiet --detach "$tap_sha"
git -C "$work/tap" merge-base --is-ancestor "$merge_sha" "$tap_sha"
[[ "$(git -C "$work/tap" rev-parse HEAD:Casks/ksail.rb)" == "$blob_sha" ]] || fail 'Pinned cask blob differs from the verified receipt'
tap_added=true
brew tap --custom-remote devantler-tech/tap "$work/tap" >>"$NATIVE_EVIDENCE_DIR/install.log" 2>&1
brew trust --tap devantler-tech/tap >>"$NATIVE_EVIDENCE_DIR/install.log" 2>&1
tap_path="$(brew --repository devantler-tech/tap)"
[[ "$(git -C "$tap_path" rev-parse HEAD)" == "$tap_sha" &&
"$(git -C "$tap_path" rev-parse HEAD:Casks/ksail.rb)" == "$blob_sha" ]] || fail 'Homebrew did not load the pinned tap'
brew info --cask --json=v2 devantler-tech/tap/ksail >"$work/cask.json"
url="https://github.com/devantler-tech/ksail/releases/download/$EXPECTED_TAG/$asset"
jq -e --arg version "$version" --arg sha "$archive_sha" --arg url "$url" '
  .casks | length == 1 and (.[0] | .version == $version and .sha256 == $sha and .url == $url)
  ' "$work/cask.json" >/dev/null || fail 'Loaded cask does not match the published native asset'
install_attempted=true
brew install --cask devantler-tech/tap/ksail >>"$NATIVE_EVIDENCE_DIR/install.log" 2>&1
[[ -x "$prefix/bin/ksail" ]] || fail 'Installed CLI binary is missing'
for path in "$prefix/etc/bash_completion.d/ksail" "$prefix/share/zsh/site-functions/_ksail" \
	"$prefix/share/fish/vendor_completions.d/ksail.fish"; do
	[[ -s "$path" ]] || fail 'Installed shell completion is missing or empty'
done
export PATH="$prefix/bin:$PATH"
[[ "$(command -v ksail)" == "$prefix/bin/ksail" ]] || fail 'A different CLI shadows the cask binary'
"${TAB_PROBE:?}" >"$work/tab.json"
jq -e '.tabObserved == true and (.observedBuffer | rtrimstr(" ")) == "ksail cluster"' \
	"$work/tab.json" >/dev/null || fail 'Interactive Tab was failed or unobserved'
retire_owned || fail 'Owned install cleanup is incomplete'
jq -n --slurpfile tab "$work/tab.json" \
	--arg tag "$EXPECTED_TAG" --arg observer "$GITHUB_SHA" --arg tap "$tap_sha" --arg blob "$blob_sha" \
	--arg merge "$merge_sha" --arg digest "$archive_sha" --argjson pr "$EXPECTED_CLI_PR" '
  {release_tag:$tag,observer_commit:$observer,cask_commit:$tap,cask_blob:$blob,cask_merge:$merge,
   cli_pr:$pr,native_asset_sha256:$digest,platform:"darwin/arm64",tab_observed:true,
   observed_buffer:$tab[0].observedBuffer,cleanup:"complete"}
  ' >"$NATIVE_EVIDENCE_DIR/receipt.json"
