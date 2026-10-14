#!/usr/bin/env bash
# Observe the fixed public targets without printing addresses, headers or raw errors.
set -euo pipefail

if [[ $# != 1 || -z "$1" ]]; then
	printf 'usage: strict-link-transport.sh <evidence-directory>\n' >&2
	exit 2
fi
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
# Prefix every match so an empty final value survives command substitution.
user_agent="$(sed -n 's/^user_agent = "\(.*\)"$/value:\1/p' "$repo_root/lychee.toml")"
user_agent="${user_agent#value:}"
if [[ -z "$user_agent" || "$user_agent" == *$'\n'* ]]; then
	printf 'ERROR: expected exactly one configured link-check user agent\n' >&2
	exit 2
fi
if ! command -v curl >/dev/null; then
	printf 'ERROR: curl is unavailable; no transport observation collected\n' >&2
	exit 2
fi

targets=(
	'https://factory.talos.dev/'
	'https://factory.talos.dev/schematics/c9078f9419961640c712a8bf2bb9174933dfcf1da383fd8ea2b7dc21493f8bac'
)
mkdir -p -- "$1"
printf '<%s>\n' "${targets[@]}" >"$1/targets.md"
printf 'observed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
for target in "${targets[@]}"; do
	result=0
	printf 'target=%s\n' "$target"
	curl --disable --silent --location --fail --output /dev/null \
		--proto '=https' --proto-redir '=https' --max-redirs 5 \
		--connect-timeout 10 --max-time 30 --user-agent "$user_agent" \
		--write-out 'http=%{http_code} dns=%{time_namelookup} connect=%{time_connect} tls=%{time_appconnect} first_byte=%{time_starttransfer} total=%{time_total} redirects=%{num_redirects} protocol=%{http_version}\n' \
		"$target" 2>/dev/null || result=$?
	printf 'curl_exit=%s\n' "$result"
done
# Collection success is not reachability success. The strict Lychee steps own that verdict.
printf 'observation=completed\n'
