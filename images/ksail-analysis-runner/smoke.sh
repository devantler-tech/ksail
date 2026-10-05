#!/bin/sh
set -eu

test "$(id -u)" = 1001
test "$(id -g)" = 1001
test "$(awk '$1 == "CapEff:" {print $2}' /proc/self/status)" = 0000000000000000
if touch /usr/local/ksail-analysis-write-probe 2>/dev/null; then
  echo 'Root filesystem unexpectedly writable' >&2
  exit 1
fi
test "$(go version)" = "go version go1.26.8 linux/$(go env GOARCH)"
test "$(node --version)" = v22.23.3
test "$(go env CGO_ENABLED)" = 1
test "$(go env GOFLAGS)" = -tags=desktop
pkg-config --exists gtk4 webkitgtk-6.0

# Mirrors the non-root init container's copy into its writable emptyDir.
cp -R /home/runner/. "${HOME}/"
mkdir -p "${HOME}/_diag" "${HOME}/_work/_tool" "${HOME}/test"
printf '{}\n' >"${HOME}/.runner"
"${HOME}/bin/Runner.Listener" --version

cd "${HOME}/test"
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
