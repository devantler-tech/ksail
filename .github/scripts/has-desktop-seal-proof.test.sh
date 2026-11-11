#!/usr/bin/env bash
# Admit only a successful, release-bound macOS worker from reviewed main.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts"
if [[ -f "$script_dir/has-desktop-seal-proof.sh" ]]; then
	cp "$script_dir/has-desktop-seal-proof.sh" "$work/scripts/"
fi
cat >"$work/scripts/cask-read-api.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
repos/devantler-tech/ksail/actions/workflows/reconcile-cask-handoff.yaml/runs\?branch=main\&status=success\&per_page=20)
	[[ "$SCENARIO" != unavailable ]] || exit 75
	jq -n --arg scenario "$SCENARIO" '
      {workflow_runs:[{id:101,run_attempt:1,status:"completed",conclusion:"success",
        event:"workflow_dispatch",head_branch:"main",head_sha:("a"*40),
        path:".github/workflows/reconcile-cask-handoff.yaml",
        head_repository:{full_name:"devantler-tech/ksail"}}]} |
      if $scenario=="wrong-branch" then .workflow_runs[0].head_branch="topic"
      elif $scenario=="wrong-event" then .workflow_runs[0].event="pull_request"
      elif $scenario=="fork-main" then .workflow_runs[0].head_repository.full_name="external/ksail"
      elif $scenario=="failed-run" then .workflow_runs[0].conclusion="failure"
      elif $scenario=="wrong-workflow" then .workflow_runs[0].path=".github/workflows/other.yaml"
      elif $scenario=="no-runs" then .workflow_runs=[] else . end' ;;
--paginate\ --slurp\ repos/devantler-tech/ksail/actions/runs/101/attempts/1/jobs\?per_page=100)
	jq -n --arg scenario "$SCENARIO" '
      [{jobs:[{name:"Reconcile Published Casks (v7.213.2)",head_sha:("a"*40),
        status:"completed",conclusion:"success"}]}] |
      if $scenario=="wrong-tag" then .[0].jobs[0].name="Reconcile Published Casks (v7.213.1)"
      elif $scenario=="legacy-job" then .[0].jobs[0].name="Reconcile Published Casks"
      elif $scenario=="bad-sha" then .[0].jobs[0].head_sha=("b"*40)
      elif $scenario=="failed-job" then .[0].jobs[0].conclusion="failure"
      elif $scenario=="skipped-job" then .[0].jobs[0].conclusion="skipped"
      elif $scenario=="duplicate-job" then .[0].jobs += [.[0].jobs[0]]
      elif $scenario=="malformed-jobs" then {} else . end' ;;
*) exit 2 ;;
esac
EOF
chmod +x "$work/scripts/cask-read-api.sh"
for scenario in valid wrong-branch wrong-event fork-main failed-run wrong-workflow no-runs wrong-tag legacy-job bad-sha failed-job skipped-job duplicate-job malformed-jobs unavailable; do
	expected=1
	[[ "$scenario" != valid ]] || expected=0
	[[ "$scenario" != unavailable ]] || expected=75
	status=0
	SCENARIO="$scenario" bash "$work/scripts/has-desktop-seal-proof.sh" v7.213.2 >"$work/output" 2>&1 || status=$?
	if [[ "$status" -ne "$expected" ]]; then
		cat "$work/output" >&2
		printf 'FAIL: seal proof %s expected %s, got %s\n' "$scenario" "$expected" "$status" >&2
		exit 1
	fi
	printf 'PASS: seal proof %s\n' "$scenario"
done
