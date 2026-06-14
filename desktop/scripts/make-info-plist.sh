#!/usr/bin/env bash
# Write the KSail.app Info.plist. Shared by make-macos-app.sh (local builds) and the desktop
# GoReleaser config (release builds) so the bundle metadata has a single source of truth.
#
# Usage: make-info-plist.sh <version> <output-path>
set -euo pipefail

version="${1:-0.0.0}"
out="${2:?output path required}"

mkdir -p "$(dirname "$out")"

cat >"$out" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>KSail</string>
  <key>CFBundleDisplayName</key><string>KSail</string>
  <key>CFBundleIdentifier</key><string>tech.devantler.ksail.desktop</string>
  <key>CFBundleExecutable</key><string>ksail-desktop</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleVersion</key><string>${version}</string>
  <key>CFBundleShortVersionString</key><string>${version}</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>CFBundleURLTypes</key>
  <array>
    <dict>
      <key>CFBundleURLName</key><string>tech.devantler.ksail.desktop</string>
      <key>CFBundleTypeRole</key><string>Viewer</string>
      <key>CFBundleURLSchemes</key>
      <array>
        <string>ksail</string>
      </array>
    </dict>
  </array>
</dict>
</plist>
PLIST
