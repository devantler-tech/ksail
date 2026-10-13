#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/repo/.github/scripts" "$tmp_dir/bin"
if [[ ! -f "$script_dir/strict-link-transport.sh" ]]; then
	printf 'FAIL: strict link transport observation is unavailable\n' >&2
	exit 1
fi
cp "$script_dir/strict-link-transport.sh" "$tmp_dir/repo/.github/scripts/"
printf 'user_agent = "controlled-agent"\n' >"$tmp_dir/repo/lychee.toml"
cat >"$tmp_dir/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >>"$CURL_CALLS"
printf 'raw private error must not escape\n' >&2
if [[ "${!#}" == 'https://factory.talos.dev/' ]]; then
	printf 'http=000 dns=0.01 connect=0.02 tls=0 first_byte=0 total=30 redirects=0 protocol=0\n'
	exit 28
fi
printf 'http=200 dns=0.01 connect=0.02 tls=0.03 first_byte=0.04 total=0.05 redirects=0 protocol=2\n'
CURL
chmod +x "$tmp_dir/bin/curl"
export CURL_CALLS="$tmp_dir/calls"
PATH="$tmp_dir/bin:$PATH" bash "$tmp_dir/repo/.github/scripts/strict-link-transport.sh" \
	"$tmp_dir/probe with spaces" >"$tmp_dir/output" 2>&1
grep -Fq 'curl_exit=28' "$tmp_dir/output"
grep -Fq 'curl_exit=0' "$tmp_dir/output"
if grep -Fq 'raw private error' "$tmp_dir/output"; then
	printf 'FAIL: raw curl errors escaped into diagnostic output\n' >&2
	exit 1
fi
[[ "$(grep -c '^https://factory.talos.dev/' "$CURL_CALLS")" == 2 ]]
[[ "$(grep -c '^--max-time$' "$CURL_CALLS")" == 2 ]]
[[ "$(grep -c '^--connect-timeout$' "$CURL_CALLS")" == 2 ]]
[[ "$(grep -c '^--proto-redir$' "$CURL_CALLS")" == 2 ]]
[[ "$(grep -c '^controlled-agent$' "$CURL_CALLS")" == 2 ]]
[[ "$(grep -A1 '^--max-time$' "$CURL_CALLS" | grep -c '^30$')" == 2 ]]
[[ "$(grep -A1 '^--connect-timeout$' "$CURL_CALLS" | grep -c '^10$')" == 2 ]]
[[ "$(grep -A1 '^--proto-redir$' "$CURL_CALLS" | grep -c '^=https$')" == 2 ]]
[[ "$(grep -c '^https://factory.talos.dev/schematics/c9078f9419961640c712a8bf2bb9174933dfcf1da383fd8ea2b7dc21493f8bac$' "$CURL_CALLS")" == 1 ]]
if grep -Eq '^(-k|--insecure|--verbose|--trace|--trace-ascii|--dump-header|--retry)$' "$CURL_CALLS"; then
	printf 'FAIL: transport probe widened or printed the request\n' >&2
	exit 1
fi
[[ "$(wc -l <"$tmp_dir/probe with spaces/targets.md" | tr -d ' ')" == 2 ]]
printf 'PASS: both fixed targets observed after a timeout with bounded private output\n'

printf 'user_agent = "first"\nuser_agent = "second"\n' >"$tmp_dir/repo/lychee.toml"
rm "$CURL_CALLS"
if PATH="$tmp_dir/bin:$PATH" bash "$tmp_dir/repo/.github/scripts/strict-link-transport.sh" \
	"$tmp_dir/invalid" >"$tmp_dir/invalid-output" 2>&1; then
	printf 'FAIL: ambiguous checker configuration accepted\n' >&2
	exit 1
fi
[[ ! -e "$CURL_CALLS" ]]
[[ ! -e "$tmp_dir/invalid/targets.md" ]]
printf 'PASS: ambiguous configuration fails before creating evidence or issuing requests\n'

for config in $'user_agent = "first"\nuser_agent = ""' $'user_agent = ""\nuser_agent = "first"'; do
	printf '%s\n' "$config" >"$tmp_dir/repo/lychee.toml"
	if PATH="$tmp_dir/bin:$PATH" bash "$tmp_dir/repo/.github/scripts/strict-link-transport.sh" \
		"$tmp_dir/empty-duplicate" >"$tmp_dir/empty-duplicate-output" 2>&1; then
		printf 'FAIL: an empty duplicate user agent was accepted\n' >&2
		exit 1
	fi
	[[ ! -e "$CURL_CALLS" ]]
	[[ ! -e "$tmp_dir/empty-duplicate/targets.md" ]]
done
printf 'PASS: empty duplicate configuration fails in either order before requests\n'
