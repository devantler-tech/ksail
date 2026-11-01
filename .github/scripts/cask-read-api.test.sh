#!/usr/bin/env bash
# Exercise the real reader and budget recovery; fake only transport and time.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
export CASK_READ_STATE="$work"
cat >"$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'call\n' >>"$CASK_READ_STATE/calls"
if [[ "$*" == 'api --include '* ]]; then
  if [[ -n "${CASK_READ_ADVANCE_TO:-}" ]]; then
    printf '%s\n' "$CASK_READ_ADVANCE_TO" >"$CASK_READ_STATE/now"
  fi
  resource=core
  [[ "$*" != 'api --include graphql '* ]] || resource=graphql
  remaining=4900
  [[ "$resource" != core || "$(cat "$CASK_READ_STATE/now")" -ge 1105 ]] || remaining=0
  printf 'HTTP/2.0 200 OK\r\nX-Ratelimit-Resource: %s\r\nX-Ratelimit-Remaining: %s\r\nX-Ratelimit-Reset: 1100\r\n\r\n' "$resource" "$remaining"
  if [[ "$resource" == core ]]; then
    printf '{"resources":{"core":{"remaining":%s,"reset":1100}}}\n' "$remaining"
  else
    printf '{"data":{"rateLimit":{"remaining":4900,"resetAt":"2030-01-01T00:00:00Z"}}}\n'
  fi
  exit 0
fi
[[ "$*" == 'api --paginate --slurp repos/devantler-tech/homebrew-tap/pulls/42/files' ]] || exit 2
if [[ ! -f "$CASK_READ_STATE/attempted" ]]; then
  touch "$CASK_READ_STATE/attempted"
  printf 'PARTIAL_PAGE_PRIVATE_SENTINEL\n'
  printf 'gh: API rate limit exceeded for installation (HTTP 403)\n' >&2
  exit 1
fi
printf '[[{"filename":"Casks/ksail.rb"}],[{"filename":"README.md"}]]\n'
EOF
cat >"$work/bin/date" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == +%s ]] || exit 2
cat "$CASK_READ_STATE/now"
EOF
cat >"$work/bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$(( $(cat "$CASK_READ_STATE/now") + $1 ))" >"$CASK_READ_STATE/now"
EOF
chmod +x "$work/bin/gh" "$work/bin/date" "$work/bin/sleep"
export PATH="$work/bin:$PATH"
printf '1000\n' >"$work/now"

reject_write() {
	local status=0
	"${script_dir}/cask-read-api.sh" "$@" >"$work/output" 2>"$work/error" || status=$?
	[[ "$status" -eq 2 && ! -f "$work/calls" ]] || {
		printf 'FAIL: mutation reached transport or was accepted\n' >&2
		exit 1
	}
}
reject_write -X PUT repos/devantler-tech/homebrew-tap/pulls/42/merge
reject_write repos/devantler-tech/homebrew-tap/pulls/42 --method=PATCH
reject_write repos/devantler-tech/homebrew-tap/issues/42 -f title=changed
reject_write graphql -f 'query=mutation { anything }'
reject_write graphql -f 'query=query Read { viewer { login } } mutation Write { anything }' -f operationName=Write
reject_write graphql --input hidden-query.json
reject_write graphql -f 'query={ viewer { login } }'
printf 'PASS: seven write or ambiguous-query forms never reach transport\n'

KSAIL_GITHUB_WRITE_BUDGET_DEADLINE=1250 \
	"${script_dir}/cask-read-api.sh" --paginate --slurp \
	repos/devantler-tech/homebrew-tap/pulls/42/files >"$work/output" 2>"$work/error"
jq -e 'map(.[]) | map(.filename) == ["Casks/ksail.rb","README.md"]' "$work/output" >/dev/null
if grep -q PARTIAL_PAGE_PRIVATE_SENTINEL "$work/output" "$work/error"; then
	printf 'FAIL: failed-read bytes escaped into output or diagnostics\n' >&2
	exit 1
fi
[[ "$(cat "$work/now")" == 1105 ]]
printf 'PASS: failed pagination is discarded before one complete retry\n'

reject_expired_recovery() {
	local recovered_at="$1" status=0
	rm -f "$work/calls" "$work/attempted" "$work/recovery-marker"
	printf '1000\n' >"$work/now"
	KSAIL_GITHUB_WRITE_BUDGET_DEADLINE=1250 CASK_READ_ADVANCE_TO="$recovered_at" \
		KSAIL_CASK_READ_RECOVERY_MARKER="$work/recovery-marker" \
		"${script_dir}/cask-read-api.sh" --paginate --slurp \
		repos/devantler-tech/homebrew-tap/pulls/42/files >"$work/output" 2>"$work/error" || status=$?
	if [[ "$status" -eq 0 || -s "$work/output" || -e "$work/recovery-marker" ||
		"$(wc -l <"$work/calls")" -ne 3 ]]; then
		printf 'FAIL: expired recovery started another read at %s\n' "$recovered_at" >&2
		exit 1
	fi
	grep -q 'BLOCKED: primary read recovery reached the original deadline' "$work/error" || {
		printf 'FAIL: expired recovery has no bounded diagnostic\n' >&2
		exit 1
	}
	if grep -q PARTIAL_PAGE_PRIVATE_SENTINEL "$work/output" "$work/error"; then
		printf 'FAIL: expired recovery exposed failed-read bytes\n' >&2
		exit 1
	fi
}
reject_expired_recovery 1250
reject_expired_recovery 1251
printf 'PASS: quota probes reaching or exceeding the deadline cannot start a retry\n'
