#!/usr/bin/env bash
# Write the KSail.app Info.plist. Shared by make-macos-app.sh (local builds) and the desktop
# GoReleaser config (release builds) so the bundle metadata has a single source of truth.
#
# Usage: make-info-plist.sh <version> <output-path> [bundle-id] [url-scheme]
#
# The bundle-id and url-scheme defaults are the deep-link contract: they must match appUniqueID
# and deepLinkScheme in desktop/deeplink.go, or single-instance enforcement and ksail:// URL
# relay silently stop working. desktop/info_plist_test.go asserts the defaults stay in sync.
set -euo pipefail

version="${1:-0.0.0}"
out="${2:?output path required}"
bundle_id="${3:-tech.devantler.ksail.desktop}"
url_scheme="${4:-ksail}"

mkdir -p "$(dirname "$out")"

cat >"$out" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>KSail</string>
  <key>CFBundleDisplayName</key><string>KSail</string>
  <key>CFBundleIdentifier</key><string>${bundle_id}</string>
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
      <key>CFBundleURLName</key><string>${bundle_id}</string>
      <key>CFBundleTypeRole</key><string>Viewer</string>
      <key>CFBundleURLSchemes</key>
      <array>
        <string>${url_scheme}</string>
      </array>
    </dict>
  </array>
</dict>
</plist>
PLIST
