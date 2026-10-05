#!/bin/sh
set -eu

test "$#" -le 1
case "${1:-}" in
  '') bootstrap=true ;;
  --live-runner) bootstrap=false ;;
  --verify-modes)
    # Image-only verification: the writable home must be the disposable mount.
    test "${HOME}" = /runner-data
    "$0"
    printf '{"agentName":"preserve-runner-configuration"}\n' >"${HOME}/.runner"
    printf '{"scheme":"ksail-smoke","data":{"marker":"preserve-runner-credentials"}}\n' >"${HOME}/.credentials"
    "$0" --live-runner
    test "$(cat "${HOME}/.runner")" = '{"agentName":"preserve-runner-configuration"}'
    test "$(cat "${HOME}/.credentials")" = '{"scheme":"ksail-smoke","data":{"marker":"preserve-runner-credentials"}}'
    printf 'PASS: live smoke preserves active runner configuration\n'
    exit 0
    ;;
  *) echo 'Unknown analysis smoke mode' >&2; exit 1 ;;
esac

test "$(id -u)" = 1001
test "$(id -g)" = 1001
test "$(awk '$1 == "CapEff:" {print $2}' /proc/self/status)" = 0000000000000000
test "$(awk '$1 == "NoNewPrivs:" {print $2}' /proc/self/status)" = 1
awk '$2 == "/" {n = split($4, options, ","); for (i = 1; i <= n; i++) if (options[i] == "ro") found = 1} END {exit !found}' /proc/mounts
if touch /usr/local/ksail-analysis-write-probe 2>/dev/null; then
  echo 'Root filesystem unexpectedly writable' >&2
  exit 1
fi
test "$(go version)" = "go version go1.26.8 linux/$(go env GOARCH)"
test "$(node --version)" = v22.23.3
test "$(go env CGO_ENABLED)" = 1
test "$(go env GOFLAGS)" = -tags=desktop
pkg-config --exists gtk4 webkitgtk-6.0

# Only the image-build smoke bootstraps a disposable home. A live job must not
# copy over its running listener or rewrite its registration/credential files.
if "$bootstrap"; then
  cp -R /home/runner/. "${HOME}/"
  mkdir -p "${HOME}/_diag" "${HOME}/_work/_tool"
  printf '{}\n' >"${HOME}/.runner"
fi
"${HOME}/bin/Runner.Listener" --version

scratch=$(mktemp -d "${HOME}/analysis-smoke.XXXXXX")
trap 'rm -rf "$scratch"' EXIT INT TERM
cd "$scratch"
cat >main.go <<'GO'
package main

/*
#cgo pkg-config: gtk4 webkitgtk-6.0
#include <gtk/gtk.h>
#include <webkit/webkit.h>
*/
import "C"

import "fmt"

func main() { fmt.Println("desktop compiler and headers work") }
GO
GOPROXY=off GOTOOLCHAIN=local go build -o desktop-compiler-check main.go
./desktop-compiler-check
printf 'PASS: non-root analysis toolchain and writable runner home\n'
