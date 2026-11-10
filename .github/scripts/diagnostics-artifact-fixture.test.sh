#!/usr/bin/env bash
# Verify that the native artifact probe executes the production programs (#7700).
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
[[ $# -le 1 ]] || {
	echo 'expected at most one immutable fixture source' >&2
	exit 2
}
fixture_source=${1-}
[[ $# == 0 || -d "$fixture_source" ]] || {
	echo 'immutable fixture source unavailable' >&2
	exit 2
}
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

compare_fixture_source() {
	local source_root=$1 fixture_mode=$2
	# Explicit failure propagation matters when a negative control calls this
	# function in an if condition, where Bash does not apply errexit.
	yq -o=json '.' "$source_root/.github/actions/diagnostics-artifact-$fixture_mode/action.yaml" |
		jq -S . >"$tmp/source-check.json" || return 1
	cmp "$tmp/$fixture_mode-generated.json" "$tmp/source-check.json"
}

for mode in baseline candidate; do
	bash "$root/.github/scripts/diagnostics-artifact-fixture.sh" "$mode" "$tmp/$mode"
	yq -o=json '.' "$tmp/$mode/action.yaml" >"$tmp/$mode.json"
	jq -S . "$tmp/$mode.json" >"$tmp/$mode-generated.json"
	# Both the reviewable local copies and the actually executed immutable
	# source must equal freshly generated programs, including failure guards.
	compare_fixture_source "$root" "$mode"
	if [[ -n "$fixture_source" ]]; then compare_fixture_source "$fixture_source" "$mode"; fi
done

# Prove stale, missing, malformed and wrong-order source never authorizes a probe.
source_control="$tmp/source-control"
source_action="$source_control/.github/actions/diagnostics-artifact-candidate"
mkdir -p "$source_action"
cp "$tmp/candidate/action.yaml" "$source_action/action.yaml"
compare_fixture_source "$source_control" candidate
jq '.runs.steps[2].run = "echo replaced"' "$tmp/candidate.json" >"$source_action/action.yaml"
if compare_fixture_source "$source_control" candidate >"$tmp/source-rejected.log" 2>&1; then
	echo 'stale fixture program was accepted' >&2
	exit 1
fi
if compare_fixture_source "$tmp/missing-source" candidate >"$tmp/source-rejected.log" 2>&1; then
	echo 'missing fixture source was accepted' >&2
	exit 1
fi
printf 'not: [yaml' >"$source_action/action.yaml"
if compare_fixture_source "$source_control" candidate >"$tmp/source-rejected.log" 2>&1; then
	echo 'malformed fixture source was accepted' >&2
	exit 1
fi
cp "$tmp/baseline/action.yaml" "$source_action/action.yaml"
if compare_fixture_source "$source_control" candidate >"$tmp/source-rejected.log" 2>&1; then
	echo 'wrong-order fixture source was accepted' >&2
	exit 1
fi
cp "$tmp/candidate/action.yaml" "$source_action/action.yaml"
compare_fixture_source "$source_control" candidate

yq -o=json '.' "$root/.github/actions/ksail-system-test/action.yaml" >"$tmp/source.json"
jq -S '[.runs.steps[] | select(.name == "🧪 Hetzner autoscaler inventory — unchanged update and retry" or .name == "📋 Save system test diagnostics" or .name == "📤 Upload system test diagnostics")]' \
	"$tmp/source.json" >"$tmp/expected.json"
jq -S '[.runs.steps[] | select((.id // "") | startswith("fixture-")) | del(.id)]' \
	"$tmp/candidate.json" >"$tmp/actual.json"
cmp "$tmp/expected.json" "$tmp/actual.json"
jq -e '.runs.steps[2].id == "fixture-save" and .runs.steps[3].id == "fixture-upload" and .runs.steps[4].id == "fixture-trial"' \
	"$tmp/baseline.json" >/dev/null
jq -e '.outputs["trial-outcome"].value == "${{ steps.fixture-trial.outcome }}" and .outputs["save-outcome"].value == "${{ steps.fixture-save.outcome }}" and .outputs["upload-outcome"].value == "${{ steps.fixture-upload.outcome }}"' \
	"$tmp/candidate.json" >/dev/null

if bash "$root/.github/scripts/diagnostics-artifact-fixture.sh" invalid "$tmp/invalid" >"$tmp/invalid.log" 2>&1; then
	echo 'invalid fixture mode was accepted' >&2
	exit 1
fi
grep -q 'expected baseline or candidate' "$tmp/invalid.log"
cp "$tmp/candidate/action.yaml" "$tmp/before.yaml"
if bash "$root/.github/scripts/diagnostics-artifact-fixture.sh" candidate "$tmp/candidate" >"$tmp/existing.log" 2>&1; then
	echo 'existing fixture was overwritten' >&2
	exit 1
fi
grep -q 'refusing to overwrite' "$tmp/existing.log"
cmp "$tmp/before.yaml" "$tmp/candidate/action.yaml"

# The native readback must refuse API failure, partial results and stale artifacts.
mkdir "$tmp/bin"
cat >"$tmp/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
read -r reads <"${FIXTURE_READS:?}"
printf '%s\n' "$((reads + 1))" >"$FIXTURE_READS"
[[ "${FIXTURE_API_STATUS:-0}" == 0 ]] || exit 1
if ((reads < FIXTURE_EMPTY_READS)); then
	printf '%s\n' '{"total_count":0,"artifacts":[]}'
	exit 0
fi
cat "${FIXTURE_REPLY:?}"
SH
cat >"$tmp/bin/sleep" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 ]]
printf '%s\n' "$1" >>"${FIXTURE_DELAYS:?}"
SH
chmod +x "$tmp/bin/gh" "$tmp/bin/sleep"
head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
jq -n --arg head "$head" '{total_count:1,artifacts:[{
  name:"system-test-diagnostics-fixture-123-1-candidate",expired:false,size_in_bytes:42,
  workflow_run:{id:123,head_sha:$head}}]}' >"$tmp/valid.json"
verify() {
	printf '0\n' >"$tmp/reads"
	: >"$tmp/delays"
	env PATH="$tmp/bin:$PATH" FIXTURE_REPLY="$tmp/reply.json" \
		FIXTURE_READS="$tmp/reads" FIXTURE_DELAYS="$tmp/delays" \
		FIXTURE_EMPTY_READS="${FIXTURE_EMPTY_READS:-0}" \
		FIXTURE_MODE="$1" PROBE_OUTCOME=failure TRIAL_OUTCOME=failure \
		SAVE_OUTCOME="$2" UPLOAD_OUTCOME="$2" EXPECTED_HEAD="$head" \
		GITHUB_REPOSITORY=fixture/fixture GITHUB_RUN_ID=123 GITHUB_RUN_ATTEMPT=1 \
		FIXTURE_API_STATUS="${FIXTURE_API_STATUS:-0}" \
		bash "$root/.github/scripts/verify-diagnostics-artifact.sh"
}
cp "$tmp/valid.json" "$tmp/reply.json"
verify candidate success
[[ "$(cat "$tmp/reads")" == 1 && ! -s "$tmp/delays" ]]
FIXTURE_EMPTY_READS=1 verify candidate success
[[ "$(cat "$tmp/reads")" == 2 && "$(cat "$tmp/delays")" == 1 ]]
printf '{"total_count":0,"artifacts":[]}' >"$tmp/reply.json"
if verify candidate success >"$tmp/rejected.log" 2>&1; then
	echo 'persistent absence was accepted for a successful upload' >&2
	exit 1
fi
[[ "$(cat "$tmp/reads")" == 4 ]]
printf '1\n2\n4\n' | cmp - "$tmp/delays"
grep -q 'artifact did not become visible after 4 reads' "$tmp/rejected.log"
if FIXTURE_API_STATUS=1 verify candidate success >"$tmp/rejected.log" 2>&1; then
	echo 'failed API read was accepted for a successful upload' >&2
	exit 1
fi
[[ "$(cat "$tmp/reads")" == 1 && ! -s "$tmp/delays" ]]
for mutation in \
	'.total_count = 2' \
	'.artifacts[0].workflow_run.head_sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
	'.artifacts[0].workflow_run.id = 124' \
	'.artifacts[0].expired = true' \
	'.artifacts[0].name = "other"' \
	'.artifacts[0].size_in_bytes = "42"' \
	'.artifacts[0].size_in_bytes = null' \
	'.artifacts[0].size_in_bytes = true' \
	'.artifacts[0].size_in_bytes = []' \
	'.artifacts[0].size_in_bytes = {}' \
	'.artifacts[0].size_in_bytes = 0' \
	'.artifacts[0].size_in_bytes = -1' \
	'.artifacts[0].size_in_bytes = 1.5' \
	'.artifacts = []'; do
	jq "$mutation" "$tmp/valid.json" >"$tmp/reply.json"
	if verify candidate success >"$tmp/rejected.log" 2>&1; then
		echo "unsafe artifact evidence accepted: $mutation" >&2
		exit 1
	fi
	[[ "$(cat "$tmp/reads")" == 1 && ! -s "$tmp/delays" ]]
done
# A successful final JSON document must not hide an earlier invalid response.
printf '{"total_count":2,"artifacts":[]}\n' >"$tmp/reply.json"
cat "$tmp/valid.json" >>"$tmp/reply.json"
if verify candidate success >"$tmp/rejected.log" 2>&1; then
	echo 'multiple API response documents were accepted' >&2
	exit 1
fi
printf '{"total_count":0,"artifacts":[]}' >"$tmp/reply.json"
verify baseline skipped
[[ "$(cat "$tmp/reads")" == 1 && ! -s "$tmp/delays" ]]
printf '{"total_count":1,"artifacts":[]}\n{"total_count":0,"artifacts":[]}' >"$tmp/reply.json"
if verify baseline skipped >"$tmp/rejected.log" 2>&1; then
	echo 'multiple API response documents authorized baseline absence' >&2
	exit 1
fi
printf '{"total_count":0,"artifacts":[]}' >"$tmp/reply.json"
if FIXTURE_API_STATUS=1 verify baseline skipped >"$tmp/rejected.log" 2>&1; then
	echo 'failed API read was accepted as baseline absence' >&2
	exit 1
fi
cp "$tmp/valid.json" "$tmp/reply.json"
if verify candidate skipped >"$tmp/rejected.log" 2>&1; then
	echo 'missing capture outcomes were accepted' >&2
	exit 1
fi
echo 'diagnostics artifact fixture: production programs retained; baseline and adverse evidence controls passed'
