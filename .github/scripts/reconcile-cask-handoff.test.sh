#!/usr/bin/env bash
# Exercise reconciliation orchestration. The existing helper suites separately
# exercise real collection, provenance, required checks and immutable delivery.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}
[[ -f "$script_dir/reconcile-cask-handoff.sh" ]] || fail 'pending casks have no automatic reconciliation entrypoint'
mkdir -p "$work/scripts"
cp "$script_dir/reconcile-cask-handoff.sh" "$work/scripts/"
cat >"$work/scripts/cask-read-api.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'read\n' >>"$CASE_STATE/reads"
case "$*" in
repos/devantler-tech/ksail/releases/latest)
  jq -n --arg scenario "$CASE_SCENARIO" '
    {id:42,tag_name:"v7.213.2",draft:false,prerelease:false,immutable:true,published_at:"2030-01-01T00:00:00Z"} |
    if $scenario == "invalid-tag" then .tag_name="../untrusted"
    elif $scenario == "draft-release" then .draft=true else . end' ;;
repos/devantler-tech/homebrew-tap/pulls\?state=open\&head=devantler-tech:goreleaser/*)
  jq -n --arg scenario "$CASE_SCENARIO" '
    [{number:42,user:{login:"devantler"},head:{repo:{full_name:"devantler-tech/homebrew-tap"}}}] |
    if $scenario == "no-pr" or $scenario == "rejected-current" then []
    elif $scenario == "ambiguous" then . + [.[0] + {number:43}]
    elif $scenario == "fork-pr" then .[0].head.repo.full_name="untrusted/tap" else . end' ;;
*) exit 2 ;;
esac
EOF
cat >"$work/scripts/find-merged-cask-pr-handoff.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$CASE_SCENARIO" != rejected-current && "$CASE_SCENARIO" != repair-current && "$CASE_SCENARIO" != observe-rejected-current ]] || exit 3
[[ "$CASE_SCENARIO" == merged || "$CASE_SCENARIO" == invalid-merge || "$CASE_SCENARIO" == observe-merged ]] || exit 1
printf '42\n'
EOF
cat >"$work/scripts/finish-cask-pr-handoff.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ " $* " == *' --tap devantler-tech/homebrew-tap '* &&
   " $* " == *' --source-repo devantler-tech/ksail '* &&
   " $* " == *' --tag v7.213.2 '* && " $* " == *' --pr 42 '* ]] || exit 2
if [[ " $* " == *' --verify-merged '* ]]; then
  printf 'verify\n' >>"$CASE_STATE/helper-calls"
  [[ "$CASE_SCENARIO" != invalid-merge ]] || exit 1
else
  printf 'deliver\n' >>"$CASE_STATE/helper-calls"
  [[ "$CASE_SCENARIO" != pending ]] || exit 75
  [[ "$CASE_SCENARIO" != failed-check ]] || exit 1
fi
printf 'PASS: full guarded evidence verified\n'
EOF
cat >"$work/scripts/verify-published-desktop-archive.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == v7.213.2 ]] || exit 2
printf 'signature\n' >>"$CASE_STATE/signatures"
[[ "$CASE_SCENARIO" != invalid-signature ]] || exit 1
EOF
chmod +x "$work/scripts/"*.sh

run_case() {
  local scenario="$1" expected="$2" ref="${3:-refs/heads/main}" status=0
  local state="$work/$scenario" args=()
  [[ "$scenario" != observe-* ]] || args+=(--observe)
  mkdir -p "$state"
  : >"$state/summary"
  CASE_STATE="$state" CASE_SCENARIO="$scenario" GITHUB_REPOSITORY=devantler-tech/ksail \
    GITHUB_REF="$ref" GITHUB_EVENT_NAME=workflow_dispatch GITHUB_STEP_SUMMARY="$state/summary" \
    bash "$work/scripts/reconcile-cask-handoff.sh" ${args[@]+"${args[@]}"} >"$state/output" 2>&1 || status=$?
  if [[ "$status" -ne "$expected" ]]; then
    cat "$state/output" >&2
    fail "$scenario expected exit $expected, received $status"
  fi
  case "$scenario" in
  delivered)
    [[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 2 ]] || fail 'both casks must use the guarded finisher'
    [[ "$(grep -c '^signature$' "$state/signatures")" -eq 1 ]] || fail 'open Desktop handoff must verify the published signature'
    grep -q 'v7.213.2' "$state/summary" || fail 'delivery has no durable release identity'
    ;;
  merged | observe-merged)
    [[ "$(grep -c '^verify$' "$state/helper-calls")" -eq 2 ]] || fail 'finder candidates lack independent provenance verification'
    ;;
  observe-pending)
    [[ ! -e "$state/helper-calls" && ! -e "$state/signatures" ]] || fail 'read-only inspection reached the delivery path'
    ;;
  invalid-signature)
    [[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 1 ]] || fail 'invalid Desktop archive reached its finisher or blocked CLI delivery'
    ;;
  invalid-merge)
    [[ "$(grep -c '^verify$' "$state/helper-calls")" -eq 2 ]] || fail 'both rejected merged candidates must be reported'
    ;;
  pending)
    grep -qi pending "$state/summary" || fail 'an unsettled cask has no pending record'
    ;;
  invalid-tag | draft-release | no-pr | fork-pr)
    [[ ! -e "$state/helper-calls" ]] || fail 'invalid publication or PR reached delivery'
    ;;
  ambiguous | rejected-current | observe-rejected-current)
    [[ ! -e "$state/helper-calls" ]] || fail 'ambiguous or rejected evidence reached delivery'
    grep -q Failed "$state/summary" || fail 'definitive failure was recorded as pending'
    ;;
  repair-current)
    [[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 2 ]] || fail 'a repair PR did not receive full guarded evaluation'
    ;;
  wrong-ref)
    [[ ! -e "$state/reads" && ! -e "$state/helper-calls" ]] || fail 'a non-main invocation reached the API'
    ;;
  esac
  printf 'PASS: %s\n' "$scenario"
}
run_case ambiguous 1
run_case rejected-current 1
run_case observe-rejected-current 1
run_case repair-current 0
run_case delivered 0
run_case merged 0
run_case invalid-merge 1
run_case invalid-signature 1
run_case observe-pending 75
run_case observe-merged 0
run_case pending 75
run_case failed-check 1
run_case invalid-tag 1
run_case draft-release 1
run_case no-pr 75
run_case fork-pr 75
run_case wrong-ref 2 refs/heads/untrusted
