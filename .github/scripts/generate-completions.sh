#!/usr/bin/env bash
# Reuse the release's native CLI after its build; cask installs only copy these files.
set -euo pipefail

if [[ $# -ne 4 ]]; then
	printf 'usage: generate-completions.sh BINARY OUTPUT TARGET HOST\n' >&2
	exit 2
fi

# A host absent from the release matrix would otherwise skip every target and
# leave GoReleaser's optional file glob empty. Refuse before any archive is made.
case "$4" in
darwin/arm64 | linux/amd64 | linux/arm64 | windows/amd64 | windows/arm64) ;;
*)
	printf 'completion generation requires a native release target\n' >&2
	exit 1
	;;
esac

# GoReleaser invokes post-build hooks for every target. Exactly the host target
# generates the shared, platform-independent scripts; foreign binaries never run.
if [[ "$3" != "$4" ]]; then exit 0; fi
binary="$1"
output="$2"
if [[ "$binary" != /* || ! -f "$binary" || ! -x "$binary" || -L "$binary" ]]; then
	printf 'completion generation requires the built native executable\n' >&2
	exit 1
fi
if [[ -e "$output" || -L "$output" ]]; then
	printf 'completion output already exists\n' >&2
	exit 1
fi

parent="$(dirname "$output")"
mkdir -p "$parent"
parent="$(cd "$parent" && pwd -P)"
work="$(mktemp -d "$parent/.ksail-completions.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir "$work/files" "$work/home"
started=$SECONDS
for shell in bash zsh fish; do
	# Completion must not inspect the runner's kubeconfig, tokens or user configuration.
	HOME="$work/home" XDG_CONFIG_HOME="$work/home" XDG_CACHE_HOME="$work/home" \
		KUBECONFIG="$work/kubeconfig" "$binary" completion "$shell" >"$work/files/ksail.$shell"
	if [[ ! -s "$work/files/ksail.$shell" ]]; then
		printf 'empty %s completion output\n' "$shell" >&2
		exit 1
	fi
	chmod 644 "$work/files/ksail.$shell"
done

# Publish all three together only after every command succeeds.
mv "$work/files" "$output"
printf 'Generated Bash, zsh and fish completions in %ss\n' "$((SECONDS - started))"
