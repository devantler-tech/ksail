#!/usr/bin/env bash
# Assemble a macOS .app bundle around the ksail-desktop binary so it behaves as a proper
# application: double-clickable, shown in the Dock, and launchable via `open -a KSail` (which the
# `ksail open desktop` command uses as its macOS fallback).
#
# Usage: make-macos-app.sh <binary-path> <output-app-path> <version> [release-notices-dir]
set -euo pipefail

binary="${1:?binary path required}"
app="${2:?output .app path required}"
version="${3:-0.0.0}"
release_notices="${4:-}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
executable="ksail-desktop"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

cp "$binary" "$app/Contents/MacOS/$executable"
chmod +x "$app/Contents/MacOS/$executable"

bash "$script_dir/make-info-plist.sh" "$version" "$app/Contents/Info.plist"

# Generate the Dock/Finder icon (referenced by CFBundleIconFile=AppIcon in the Info.plist).
bash "$script_dir/make-icns.sh" "$script_dir/../resources/icon.png" "$app/Contents/Resources/AppIcon.icns"

if [[ -n "$release_notices" ]]; then
  cp "$script_dir/../../LICENSE" "$app/Contents/Resources/LICENSE"
  cp -R "$release_notices" "$app/Contents/Resources/GoNotices"
  codesign --force --sign - "$app"
fi

echo "built $app (version $version)"
