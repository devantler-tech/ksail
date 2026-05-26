#!/usr/bin/env bash
# Generate the KSail.app icon (AppIcon.icns) from a square source PNG. Shared by make-macos-app.sh
# (local builds) and the desktop GoReleaser config (release builds) so the bundle icon has a single
# source of truth. macOS only: relies on sips + iconutil, which is fine because the .app bundle is
# only ever built on macOS.
#
# Usage: make-icns.sh <source-png> <output-icns>
set -euo pipefail

src="${1:?source PNG path required}"
out="${2:?output .icns path required}"

for tool in sips iconutil; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "make-icns.sh: '$tool' not found; the KSail.app icon can only be built on macOS" >&2
    exit 1
  }
done

mkdir -p "$(dirname "$out")"

iconset="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$iconset"
trap 'rm -rf "$(dirname "$iconset")"' EXIT

# The standard macOS icon sizes (point size + @1x/@2x). sips downsamples crisply; the larger slots
# upsample the source where needed.
for spec in \
  "16:icon_16x16" \
  "32:icon_16x16@2x" \
  "32:icon_32x32" \
  "64:icon_32x32@2x" \
  "128:icon_128x128" \
  "256:icon_128x128@2x" \
  "256:icon_256x256" \
  "512:icon_256x256@2x" \
  "512:icon_512x512" \
  "1024:icon_512x512@2x"; do
  size="${spec%%:*}"
  name="${spec##*:}"
  sips -z "$size" "$size" "$src" --out "$iconset/$name.png" >/dev/null
done

iconutil --convert icns "$iconset" --output "$out"

echo "built $out from $src"
