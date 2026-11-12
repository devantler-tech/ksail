#!/usr/bin/env bash
# Exercise publication recovery with real macOS resource seals and ZIP extraction.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
[[ "$(uname -s)" == Darwin ]] || exit 2
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/KSail.app/Contents/MacOS" "$work/KSail.app/Contents/Resources" "$work/bin"
cp /usr/bin/true "$work/KSail.app/Contents/MacOS/KSail"
cat >"$work/KSail.app/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>KSail</string>
<key>CFBundleIdentifier</key><string>tech.devantler.ksail.signature-fixture</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
EOF
printf 'Original license\n' >"$work/KSail.app/Contents/Resources/LICENSE"
codesign --force --sign - --timestamp=none "$work/KSail.app" 2>"$work/sign.log"
ditto -c -k --keepParent "$work/KSail.app" "$work/valid.zip"
printf 'Changed after signing\n' >"$work/KSail.app/Contents/Resources/LICENSE"
ditto -c -k --keepParent "$work/KSail.app" "$work/invalid.zip"

# Replace transport only; keep the real digest, extraction and signature checks.
cat >"$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'api repos/devantler-tech/ksail/releases/tags/v7.212.40' ]] || exit 2
cat "$RELEASE_FIXTURE"
EOF
cat >"$work/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output="" url="" max_time=""
while (($#)); do
	case "$1" in
	--output) output="$2"; shift 2 ;;
	--max-time) max_time="$2"; shift 2 ;;
	https://*) url="$1"; shift ;;
	*) shift ;;
	esac
done
[[ "$url" == https://github.com/devantler-tech/ksail/releases/download/v7.212.40/KSail_7.212.40_darwin_arm64.zip && -n "$output" ]] || exit 2
[[ -z "${DOWNLOAD_BOUND:-}" ]] || printf '%s\n' "$max_time" >"$DOWNLOAD_BOUND"
cp "$ARCHIVE_FIXTURE" "$output"
exit "${DOWNLOAD_STATUS:-0}"
EOF
cat >"$work/bin/timeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" != -k ]] || shift 2
shift
exec "$@"
EOF
cat >"$work/bin/date" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == +%s && -n "${TEST_NOW:-}" ]]; then
	printf '%s\n' "$TEST_NOW"
else
	/usr/bin/date "$@"
fi
EOF
chmod +x "$work/bin/gh" "$work/bin/curl" "$work/bin/timeout" "$work/bin/date"
export PATH="$work/bin:$PATH"
export RELEASE_FIXTURE="$work/release.json" ARCHIVE_FIXTURE="$work/valid.zip"

# Describe the actual fixture publication without using production parsing code.
publication() {
	local digest
	digest="$(shasum -a 256 "$1" | awk '{print $1}')"
	jq -n --arg digest "sha256:$digest" '{tag_name:"v7.212.40",draft:false,
    immutable:true,published_at:"2026-11-11T00:00:00Z",
    assets:[{name:"KSail_7.212.40_darwin_arm64.zip",size:1,digest:$digest}]}' >"$RELEASE_FIXTURE"
}

publication "$ARCHIVE_FIXTURE"
bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/valid.log" 2>&1
grep -q '^PASS: published Desktop archive' "$work/valid.log"
printf 'PASS: valid immutable publication\n'

status=0
DOWNLOAD_STATUS=28 bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/timeout.log" 2>&1 || status=$?
[[ "$status" -eq 75 ]] || {
	printf 'FAIL: a bounded archive download timeout must remain pending, got %s\n' "$status" >&2
	exit 1
}
if grep -q '^PASS:' "$work/timeout.log"; then
	printf 'FAIL: a timed-out archive was reported verified\n' >&2
	exit 1
fi
printf 'PASS: bounded archive download timeout remains pending\n'

export DOWNLOAD_BOUND="$work/download-bound"
TEST_NOW=1000 KSAIL_GITHUB_WRITE_BUDGET_DEADLINE=1030 \
	bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/bounded.log" 2>&1
bound="$(<"$DOWNLOAD_BOUND")"
[[ "$bound" == 30 ]] || {
	printf 'FAIL: archive download exceeded the remaining shared deadline\n' >&2
	exit 1
}
grep -q '^PASS: published Desktop archive' "$work/bounded.log"
printf 'PASS: actual archive verification uses the remaining shared deadline\n'

rm -f "$DOWNLOAD_BOUND"
status=0
TEST_NOW=1000 KSAIL_GITHUB_WRITE_BUDGET_DEADLINE=999 \
	bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/expired.log" 2>&1 || status=$?
[[ "$status" -eq 75 && ! -e "$DOWNLOAD_BOUND" ]] || {
	printf 'FAIL: an exhausted shared deadline reached the archive download\n' >&2
	exit 1
}
printf 'PASS: an expired deadline cannot start an archive download\n'

ARCHIVE_FIXTURE="$work/invalid.zip"
if bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/digest.log" 2>&1; then
	printf 'FAIL: download with a different checksum was accepted\n' >&2
	exit 1
fi
grep -q 'does not match its published digest' "$work/digest.log"
printf 'PASS: changed download rejected\n'

# Even a checksum-matching publication must fail when its signed resources changed.
publication "$ARCHIVE_FIXTURE"
if bash "${script_dir}/verify-published-desktop-archive.sh" v7.212.40 >"$work/seal.log" 2>&1; then
	printf 'FAIL: checksum-matching invalid resource seal was accepted\n' >&2
	exit 1
fi
grep -q 'sealed resource is missing or invalid' "$work/seal.log"
printf 'PASS: checksum-matching invalid resource seal rejected\n'
