#!/usr/bin/env bash
# Replay the released scanner using inputs derived from this repository's caller.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
catalogue="${1:?catalogue checkout required}"
mode="${2:-native}"
[[ "$mode" == native || "$mode" == --check-fixtures ]] || exit 2
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fail() {
	echo "::error::$*" >&2
	exit 1
}
yq -o=json '.' "$root/.github/workflows/todos.yaml" >"$work/caller.json"
jq -e '
  (.jobs | keys) == ["todos"] and
  (.jobs.todos | keys) == ["secrets", "uses", "with"] and
  .jobs.todos.with == {"exclude-vendored":true} and
  .jobs.todos.secrets == {"APP_PRIVATE_KEY":"${{ secrets.APP_PRIVATE_KEY }}"} and
  .permissions == {"contents":"read", "issues":"write"}
' "$work/caller.json" >/dev/null || fail 'Consumer declaration changed outside the reviewed adoption'
reference="$(jq -er '.jobs.todos.uses' "$work/caller.json")"
[[ "$reference" =~ ^devantler-tech/\.github/\.github/workflows/scan-for-todo-comments\.yaml@([0-9a-f]{40})$ ]] ||
	fail 'Consumer must use the immutable canonical workflow'
revision="${BASH_REMATCH[1]}"
[[ "$(git -C "$catalogue" rev-parse HEAD)" == "$revision" ]] || fail 'Catalogue checkout does not match consumer revision'
status="$(git -C "$catalogue" status --porcelain)" || fail 'Could not establish catalogue checkout status'
[[ -z "$status" ]] || fail 'Catalogue checkout must be clean'
yq -o=json '.' "$catalogue/.github/workflows/scan-for-todo-comments.yaml" >"$work/shared.json"
jq -e '
  .jobs.todos.if == "${{ !inputs.dry-run }}" and
  (.jobs.todos.steps | length) == 5 and
  .jobs.todos.steps[2].with == {
    "repository":"${{ job.workflow_repository }}", "ref":"${{ job.workflow_sha }}",
    "path":".devantler-tech-actions", "persist-credentials":false
  } and
  .jobs.todos.steps[3].uses == "./.devantler-tech-actions/actions/create-issues-from-todos" and
  .jobs.todos.steps[3].with.ignore == "${{ inputs.ignore }}" and
  .jobs.todos.steps[3].with["exclude-vendored"] == "${{ inputs.exclude-vendored }}" and
  (.jobs.todos.steps[3] | has("if") | not) and
  .on.workflow_call.inputs.ignore.default == "" and
  .on.workflow_call.inputs["exclude-vendored"].default == false
' "$work/shared.json" >/dev/null || fail 'Shared production delegation no longer matches reviewed forwarding'
jq --slurpfile caller "$work/caller.json" --slurpfile shared "$work/shared.json" '
  [ .[] | select(.Name == "opted-in vendor filter excludes root directories only") |
    .Ignore = (if $caller[0].jobs.todos.with | has("ignore")
      then $caller[0].jobs.todos.with.ignore else $shared[0].on.workflow_call.inputs.ignore.default end) |
    .ExcludeVendored = ((if $caller[0].jobs.todos.with | has("exclude-vendored")
      then $caller[0].jobs.todos.with["exclude-vendored"]
      else $shared[0].on.workflow_call.inputs["exclude-vendored"].default end) | tostring)
  ] | if length == 1 then . else error("exact released vendor scenario required") end
' "$catalogue/.github/tests/todo-scanner/cases.json" >"$work/healthy.json"
runner="$catalogue/.github/tests/test-todo-scanner.sh"
bash "$runner" --check-fixtures "$work/healthy.json"
[[ "$mode" != --check-fixtures ]] || {
	echo 'PASS: consumer inputs bind to the released native fixture'
	exit 0
}
bash "$runner" --cases "$work/healthy.json"
reject() {
	local name="$1" diagnostic="$2" file="$3"
	if bash "$runner" --cases "$file" >"$work/$name.log" 2>&1; then
		fail "Native control unexpectedly passed: $name"
	fi
	# Require a replay diagnostic; an image pull or build failure is never evidence.
	grep -F "$diagnostic" "$work/$name.log" >/dev/null || {
		cat "$work/$name.log"
		fail "Native control did not reach the intended boundary: $name"
	}
	echo "PASS: native control rejected $name"
}
jq '.[0].ExcludeVendored = "false"' "$work/healthy.json" >"$work/disabled.json"
reject disabled 'wrong method or path' "$work/disabled.json"
jq '.[0].Ignore = "^"' "$work/healthy.json" >"$work/overbroad.json"
reject overbroad 'missing requests:' "$work/overbroad.json"
jq '.[0].Operations[1].Body |= (fromjson | .title = "corrupt expected title" | tojson)' \
	"$work/healthy.json" >"$work/payload.json"
reject payload 'issue payload differs' "$work/payload.json"
echo 'PASS: native consumer vendor filtering and three effective controls'
