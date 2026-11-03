#!/usr/bin/env bash
# Refuse missing outcomes and incomplete API evidence; never infer absence from a failed read.
set -euo pipefail
case "${FIXTURE_MODE:?}" in baseline | candidate) ;; *) exit 2 ;; esac
[[ "${PROBE_OUTCOME:?}" == failure && "${TRIAL_OUTCOME:?}" == failure ]]
if [[ "$FIXTURE_MODE" == candidate ]]; then
  [[ "${SAVE_OUTCOME:?}" == success && "${UPLOAD_OUTCOME:?}" == success ]]
else
  [[ "${SAVE_OUTCOME:?}" == skipped && "${UPLOAD_OUTCOME:?}" == skipped ]]
fi
[[ "${EXPECTED_HEAD:?}" =~ ^[0-9a-f]{40}$ ]]
[[ "${GITHUB_RUN_ID:?}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:?}" =~ ^[0-9]+$ ]]
name="system-test-diagnostics-fixture-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$FIXTURE_MODE"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
gh api --method GET "repos/${GITHUB_REPOSITORY:?}/actions/runs/$GITHUB_RUN_ID/artifacts" \
  -f name="$name" -f per_page=100 >"$tmp"
jq -e --arg name "$name" --arg mode "$FIXTURE_MODE" --arg head "$EXPECTED_HEAD" \
  --argjson run "$GITHUB_RUN_ID" '
  (.total_count | type == "number") and (.artifacts | type == "array") and
  .total_count == (.artifacts | length) and
  if $mode == "baseline" then .total_count == 0 else
    .total_count == 1 and all(.artifacts[];
      .name == $name and .expired == false and .size_in_bytes > 0 and
      .workflow_run.id == $run and .workflow_run.head_sha == $head)
  end' "$tmp" >/dev/null
printf 'Native diagnostics artifact %s: complete outcomes and artifact identity verified\n' "$FIXTURE_MODE"
