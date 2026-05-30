#!/usr/bin/env bash
# Regenerate the KSail documentation demo recordings from the *.tape scripts in
# this directory using VHS (https://github.com/charmbracelet/vhs).
#
#   ./render.sh                 # render every *.tape
#   ./render.sh cluster-init    # render only cluster-init.tape
#
# Each tape runs inside a throwaway scratch directory, so demo commands such as
# `ksail cluster init` operate on a clean project and NO setup (cd/source/clear)
# is ever typed on screen. VHS writes <name>.gif/.mp4 into that scratch dir and
# we move the result into ../public/demos/, which is committed so the published
# docs site can serve the recordings without a build-time render step.
#
# Requires vhs (brew install vhs — pulls in ttyd + ffmpeg). Tapes that create a
# real cluster (e.g. cluster-lifecycle.tape) also need Docker running.
set -euo pipefail

cd "$(dirname "$0")"
DEMOS_DIR="$PWD"
OUT_DIR="$DEMOS_DIR/../public/demos"

if ! command -v vhs >/dev/null 2>&1; then
  echo "vhs not found — install it with: brew install vhs" >&2
  exit 1
fi

echo "==> building ksail"
go build -o "$DEMOS_DIR/.bin/ksail" ../..
export PATH="$DEMOS_DIR/.bin:$PATH"
mkdir -p "$OUT_DIR"

# Safety net: cluster-creating tapes (e.g. cluster-lifecycle) name their cluster
# "ksail-demo" and delete it themselves, but if a render aborts mid-way we must
# never leave a cluster running. --force avoids the interactive confirmation.
trap '"$DEMOS_DIR/.bin/ksail" cluster delete --name ksail-demo --force >/dev/null 2>&1 || true' EXIT

# Warm the OS page cache: the first cold run of the ~300MB binary is ~3s, which
# would otherwise race the Sleep timings in the (cache-warm) recordings.
( cd "$(mktemp -d)" && ksail cluster init >/dev/null 2>&1 || true )

# Shrink a rendered GIF. Lossless first; long, heavy-scroll demos (lifecycle)
# stay multi-MB even so, so those get downscaled + mild lossy — the matching
# .mp4 keeps full resolution for crisp embedding where size matters less.
optimize_gif() {
  local f="$1" size
  command -v gifsicle >/dev/null 2>&1 || return 0
  gifsicle -O3 -b "$f"
  size=$(wc -c < "$f")
  if [ "$size" -gt 1258291 ]; then # > 1.2 MiB
    gifsicle -O3 --lossy=60 --resize-width 900 -b "$f"
  fi
}

shopt -s nullglob
if [ "$#" -gt 0 ]; then
  tapes=()
  for name in "$@"; do tapes+=("${name%.tape}.tape"); done
else
  tapes=(*.tape)
fi

for tape in "${tapes[@]}"; do
  [ -f "$tape" ] || { echo "skip: $tape not found" >&2; continue; }
  name="${tape%.tape}"
  echo "==> rendering $tape"
  scratch="$(mktemp -d)"
  ( cd "$scratch" && vhs "$DEMOS_DIR/$tape" )
  for ext in gif mp4 webm; do
    [ -f "$scratch/$name.$ext" ] && mv -f "$scratch/$name.$ext" "$OUT_DIR/$name.$ext"
  done
  rm -rf "$scratch"
  [ -f "$OUT_DIR/$name.gif" ] && optimize_gif "$OUT_DIR/$name.gif"
done

echo "==> done — output in ${OUT_DIR}"
