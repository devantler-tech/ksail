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
# The one artifact this run's upload must have produced.
# shellcheck disable=SC2016 # $name, $head and $run are jq variables.
expected='def expected($name; $head; $run):
  (.id | type == "number") and .id > 0 and (.id | floor) == .id and
  .name == $name and .expired == false and
  (.size_in_bytes | type == "number") and .size_in_bytes > 0 and
  (.size_in_bytes | floor) == .size_in_bytes and
  .workflow_run.id == $run and .workflow_run.head_sha == $head;'
# After a successful upload the filtered listing is briefly inconsistent: it may be
# empty, count 1 before the artifact is listed (#8475), or list the expected
# artifact while still counting 0 (#8583). Only those complete candidate responses
# may wait; errors and wrong evidence fail.
for delay in 1 2 4 0; do
	gh api --method GET "repos/${GITHUB_REPOSITORY:?}/actions/runs/$GITHUB_RUN_ID/artifacts" \
		-f name="$name" -f per_page=100 >"$tmp"
	if [[ "$FIXTURE_MODE" == baseline ]] ||
		! jq -es --arg name "$name" --arg head "$EXPECTED_HEAD" --argjson run "$GITHUB_RUN_ID" \
			"$expected"' length == 1 and (.[0] | (.artifacts | type == "array") and (
			  ((.total_count == 0 or .total_count == 1) and .artifacts == []) or
			  (.total_count == 0 and (.artifacts | length) == 1 and
			    (.artifacts[0] | expected($name; $head; $run)))))' "$tmp" >/dev/null; then
		break
	fi
	if ((delay == 0)); then
		echo 'Native diagnostics artifact candidate: uploaded artifact did not become visible after 4 reads' >&2
		exit 1
	fi
	sleep "$delay"
done
# Name the rejected evidence: a silent exit hid which field disagreed (#8475).
reject() {
	echo "Native diagnostics artifact $FIXTURE_MODE: artifact evidence rejected (expected name=$name run=$GITHUB_RUN_ID head=$EXPECTED_HEAD); API response:" >&2
	jq -c '.' "$tmp" >&2 || cat "$tmp" >&2
	exit 1
}
jq -es --arg name "$name" --arg mode "$FIXTURE_MODE" --arg head "$EXPECTED_HEAD" \
	--argjson run "$GITHUB_RUN_ID" "$expected"'
  length == 1 and (.[0] |
  (.total_count | type == "number") and (.artifacts | type == "array") and
  .total_count == (.artifacts | length) and
  if $mode == "baseline" then .total_count == 0 else
    .total_count == 1 and all(.artifacts[]; expected($name; $head; $run))
  end)' "$tmp" >/dev/null || reject
# Hand the readback the exact verified identity: a second by-name lookup can
# miss an artifact this listing already proved (ci run 45304265375).
if [[ "$FIXTURE_MODE" == candidate ]]; then
	printf 'artifact-id=%s\n' "$(jq -r '.artifacts[0].id' "$tmp")" >>"${GITHUB_OUTPUT:?}"
fi
printf 'Native diagnostics artifact %s: complete outcomes and artifact identity verified\n' "$FIXTURE_MODE"
