#!/usr/bin/env bash
# Verify that the native artifact probe executes the production programs (#7700).
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for mode in baseline candidate; do
	bash "$root/.github/scripts/diagnostics-artifact-fixture.sh" "$mode" "$tmp/$mode"
	yq -o=json '.' "$tmp/$mode/action.yaml" >"$tmp/$mode.json"
	# GitHub's immutable self-repository reference loads these reviewed files,
	# not an action created in the checkout. Compare structurally so formatting
	# cannot conceal stale generated programs or different failure conditions.
	yq -o=json '.' "$root/.github/actions/diagnostics-artifact-$mode/action.yaml" |
		jq -S . >"$tmp/$mode-committed.json"
	jq -S . "$tmp/$mode.json" >"$tmp/$mode-generated.json"
	cmp "$tmp/$mode-committed.json" "$tmp/$mode-generated.json"
done
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
[[ "${FIXTURE_API_STATUS:-0}" == 0 ]] || exit 1
cat "${FIXTURE_REPLY:?}"
SH
chmod +x "$tmp/bin/gh"
head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
jq -n --arg head "$head" '{total_count:1,artifacts:[{
  name:"system-test-diagnostics-fixture-123-1-candidate",expired:false,size_in_bytes:42,
  workflow_run:{id:123,head_sha:$head}}]}' >"$tmp/valid.json"
verify() {
	env PATH="$tmp/bin:$PATH" FIXTURE_REPLY="$tmp/reply.json" \
		FIXTURE_MODE="$1" PROBE_OUTCOME=failure TRIAL_OUTCOME=failure \
		SAVE_OUTCOME="$2" UPLOAD_OUTCOME="$2" EXPECTED_HEAD="$head" \
		GITHUB_REPOSITORY=fixture/fixture GITHUB_RUN_ID=123 GITHUB_RUN_ATTEMPT=1 \
		FIXTURE_API_STATUS="${FIXTURE_API_STATUS:-0}" \
		bash "$root/.github/scripts/verify-diagnostics-artifact.sh"
}
cp "$tmp/valid.json" "$tmp/reply.json"
verify candidate success
for mutation in \
	'.total_count = 2' \
	'.artifacts[0].workflow_run.head_sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
	'.artifacts[0].workflow_run.id = 124' \
	'.artifacts[0].expired = true' \
	'.artifacts[0].name = "other"' \
	'.artifacts = []'; do
	jq "$mutation" "$tmp/valid.json" >"$tmp/reply.json"
	if verify candidate success >"$tmp/rejected.log" 2>&1; then
		echo "unsafe artifact evidence accepted: $mutation" >&2
		exit 1
	fi
done
printf '{"total_count":0,"artifacts":[]}' >"$tmp/reply.json"
verify baseline skipped
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
