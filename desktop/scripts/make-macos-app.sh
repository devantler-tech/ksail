#!/usr/bin/env bash
# Assemble a macOS .app bundle around the ksail-desktop binary so it behaves as a proper
# application: double-clickable, shown in the Dock, and launchable via `open -a KSail` (which the
# `ksail desktop` command uses as its macOS fallback).
#
# Usage: make-macos-app.sh <binary-path> <output-app-path> <version>
set -euo pipefail

binary="${1:?binary path required}"
app="${2:?output .app path required}"
version="${3:-0.0.0}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
executable="ksail-desktop"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

cp "$binary" "$app/Contents/MacOS/$executable"
chmod +x "$app/Contents/MacOS/$executable"

bash "$script_dir/make-info-plist.sh" "$version" "$app/Contents/Info.plist"

echo "built $app (version $version)"
