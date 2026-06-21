#!/usr/bin/env bash
# Build the web UI (web/ui) and stage it into pkg/webui/dist so `go build` embeds it via
# pkg/webui/embed.go — served by the operator, `ksail open web`, and the desktop app on the same
# origin as /api (no reverse proxy). Built assets are gitignored; the .gitkeep keeps the
# embed directory present so the go:embed directive always resolves.
#
# Single source of truth for the staging recipe. Called from:
#   - Makefile (`make ui`)
#   - .github/workflows/desktop.yaml + cd.yaml (via .github/actions/setup-desktop-build)
#   - .goreleaser.yaml + .goreleaser.desktop.yaml (before hooks)
#   - .github/actions/operator-chart-e2e/action.yml
#
# Usage: stage-webui.sh (no args; works from any CWD — it cds to the repo root itself)
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

npm --prefix web/ui ci
npm --prefix web/ui run build
rm -rf pkg/webui/dist
mkdir -p pkg/webui/dist
touch pkg/webui/dist/.gitkeep
cp -R web/ui/dist/. pkg/webui/dist/
