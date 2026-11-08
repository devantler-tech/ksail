#!/usr/bin/env bash
# Disposable Linux CI host only; Kind's documented inotify minimums are shared
# by the outer nodes and the nested node containers, rather than namespaced.
set -euo pipefail
keys=(fs.inotify.max_user_instances fs.inotify.max_user_watches)
minimums=(512 524288)
observations=()
for key in "${keys[@]}"; do
	observed="$(sysctl -n "$key")"
	[[ "$observed" =~ ^[1-9][0-9]{0,9}$ ]] || {
		echo 'Invalid nested Kind host capacity reading' >&2
		exit 1
	}
	observations+=("$observed")
done

# Complete both reads before changing either setting, and never lower a host's
# existing capacity. Read back the effective value even when no write is needed.
for index in "${!keys[@]}"; do
	key=${keys[$index]}
	minimum=${minimums[$index]}
	observed=${observations[$index]}
	if ((observed < minimum)); then
		sudo -n sysctl -w "$key=$minimum"
	fi
	actual="$(sysctl -n "$key")"
	if ! [[ "$actual" =~ ^[1-9][0-9]{0,9}$ ]] || ((actual < minimum || actual < observed)); then
		echo 'Nested Kind host capacity readback failed' >&2
		exit 1
	fi
	printf 'Nested Kind host capacity: %s %s -> %s\n' "$key" "$observed" "$actual"
done
