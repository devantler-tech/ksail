#!/usr/bin/env bash
# Reconcile the latest published release through the same guarded cask finisher.
# Run only from reviewed main, serialized with native CD by the caller workflow.
set -euo pipefail
observe=false
if [[ "$#" -eq 1 && "$1" == --observe ]]; then observe=true; shift; fi
if [[ "$#" -ne 0 || "${GITHUB_REPOSITORY:-}" != devantler-tech/ksail ||
  "${GITHUB_REF:-}" != refs/heads/main ||
  ("${GITHUB_EVENT_NAME:-}" != schedule && "${GITHUB_EVENT_NAME:-}" != workflow_dispatch) ]]; then
  printf 'ERROR: cask reconciliation requires this repository on main\n' >&2
  exit 2
fi
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
now="$(date +%s)"
[[ "$now" =~ ^[1-9][0-9]{0,9}$ ]] || exit 1
export KSAIL_GITHUB_WRITE_BUDGET_DEADLINE="$((now + 300))"
export KSAIL_CASK_CHECK_DEADLINE="$((now + 300))"
read_api() { "$script_dir/cask-read-api.sh" "$@"; }
read_api repos/devantler-tech/ksail/releases/latest >"$work/release.json"
if ! jq -e '
  .tag_name | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")
' "$work/release.json" >/dev/null 2>&1 ||
  ! jq -e '.draft == false and .prerelease == false and .immutable == true
  and (.published_at | type == "string" and length > 0)
  and (.id | type == "number" and . > 0 and . == floor)
' "$work/release.json" >/dev/null 2>&1; then
  printf 'ERROR: latest release identity is incomplete or unpublished\n' >&2
  exit 1
fi
tag="$(jq -r '.tag_name' "$work/release.json")"
pending=0 failed=0
record_result() {
  local name="$1" status="$2" result
  case "$status" in
  0) result=Delivered ;;
  75)
    result=Pending
    pending=1
    ;;
  *)
    result=Failed
    failed=1
    ;;
  esac
  printf '%s cask %s for %s\n' "$result" "$name" "$tag"
  printf -- '- %s cask %s for %s\n' "$result" "$name" "$tag" >>"$GITHUB_STEP_SUMMARY"
}
for name in ksail ksail-desktop; do
  mkdir -p "$work/$name"
  mode_args=(--checks-wait 120)
  if pr="$("$script_dir/find-merged-cask-pr-handoff.sh" \
    --tap devantler-tech/homebrew-tap --cask-name "$name" \
    --source-repo devantler-tech/ksail --tag "$tag" --output-dir "$work/$name")"; then
    # The finder supplies a candidate. Independently verify its programmed
    # provenance and publication before accepting it as a delivered cask.
    mode_args+=(--verify-merged)
  else
    if [[ "$observe" == true ]]; then
      record_result "$name" 75
      continue
    fi
    status=0
    read_api "repos/devantler-tech/homebrew-tap/pulls?state=open&head=devantler-tech:goreleaser/${name}&per_page=100" \
      >"$work/open.json" || status=$?
    if [[ "$status" -ne 0 ]]; then
      record_result "$name" "$status"
      continue
    fi
    if ! jq -e 'type == "array"' "$work/open.json" >/dev/null 2>&1; then
      record_result "$name" 1
      continue
    fi
    pr="$(jq -r '[.[] | select(.head.repo.full_name == "devantler-tech/homebrew-tap"
      and .user.login == "devantler")] |
      if length == 1 then .[0].number else empty end' "$work/open.json")"
  fi
  if [[ ! "$pr" =~ ^[1-9][0-9]*$ ]]; then
    record_result "$name" 75
    continue
  fi
  status=0
  # A deferred native job may not have reached the archive guard. Preserve it
  # before every open Desktop handoff, on the caller's macOS runner.
  if [[ "$name" == ksail-desktop && "$observe" == false &&
    " ${mode_args[*]} " != *' --verify-merged '* ]]; then
    "$script_dir/verify-published-desktop-archive.sh" "$tag" || status=$?
    if [[ "$status" -ne 0 ]]; then
      record_result "$name" "$status"
      continue
    fi
  fi
  "$script_dir/finish-cask-pr-handoff.sh" \
    --tap devantler-tech/homebrew-tap --pr "$pr" --cask-name "$name" \
    --source-repo devantler-tech/ksail --tag "$tag" "${mode_args[@]}" || status=$?
  record_result "$name" "$status"
done
if [[ "$failed" -ne 0 ]]; then
  printf 'ERROR: a cask failed guarded reconciliation; maintainer attention is required\n' >&2
  exit 1
fi
if [[ "$pending" -ne 0 ]]; then
  printf 'ERROR: cask delivery remains pending; no unverified delivery was accepted\n' >&2
  exit 75
fi
