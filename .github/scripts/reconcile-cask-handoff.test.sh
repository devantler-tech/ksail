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
    {id:42,tag_name:"v7.213.2",draft:false,prerelease:false,immutable:true,published_at:"2030-01-01T00:00:00Z",
      assets:[{name:"KSail_7.213.2_darwin_arm64.zip",digest:("sha256:"+("a"*64))}]} |
    if $scenario == "invalid-tag" then .tag_name="../untrusted"
    elif $scenario == "draft-release" then .draft=true else . end' ;;
repos/devantler-tech/homebrew-tap/pulls\?state=open\&head=devantler-tech:goreleaser/*)
  name="${*##*goreleaser/}"
  name="${name%%&*}"
  release=v7.213.2
  [[ "$CASE_SCENARIO" != stale-title && "$CASE_SCENARIO" != title-write-failed ]] || release=v7.213.1
  jq -n --arg scenario "$CASE_SCENARIO" --arg title "chore(cask): update ${name} to ${release}" '
    [{number:42,title:$title,user:{login:"devantler"},head:{repo:{full_name:"devantler-tech/homebrew-tap"}},
      labels:[{name:"automation"},{name:"dependencies"}]}] |
    if $scenario == "missing-labels" or $scenario == "label-write-failed" then .[0].labels=[{name:"automation"}]
    elif $scenario == "no-pr" or $scenario == "rejected-current" then []
    elif $scenario == "ambiguous" then . + [.[0] + {number:43}]
    elif $scenario == "fork-pr" then .[0].head.repo.full_name="untrusted/tap" else . end' ;;
*) exit 2 ;;
esac
EOF
cat >"$work/scripts/find-merged-cask-pr-handoff.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$CASE_SCENARIO" != rejected-current && "$CASE_SCENARIO" != repair-current ]] || exit 3
[[ "$CASE_SCENARIO" == merged || "$CASE_SCENARIO" == invalid-merge || "$CASE_SCENARIO" == merged-invalid-signature ]] || exit 1
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
[[ "$CASE_SCENARIO" != invalid-signature && "$CASE_SCENARIO" != merged-invalid-signature ]] || exit 1
EOF
chmod +x "$work/scripts/"*.sh
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CASE_STATE/gh-calls"
[[ "$CASE_SCENARIO" != title-write-failed ]] || exit 1
[[ "$CASE_SCENARIO" != label-write-failed || "$*" != *'/labels '* ]] || exit 1
EOF
chmod +x "$work/bin/gh"

run_case() {
	local scenario="$1" expected="$2" ref="${3:-refs/heads/main}" status=0
	local state="$work/$scenario"
	mkdir -p "$state"
	: >"$state/summary"
	PATH="$work/bin:$PATH" CASE_STATE="$state" CASE_SCENARIO="$scenario" GITHUB_REPOSITORY=devantler-tech/ksail \
		GITHUB_REF="$ref" GITHUB_EVENT_NAME=workflow_dispatch GITHUB_STEP_SUMMARY="$state/summary" \
		bash "$work/scripts/reconcile-cask-handoff.sh" >"$state/output" 2>&1 || status=$?
	if [[ "$status" -ne "$expected" ]]; then
		cat "$state/output" >&2
		fail "$scenario expected exit $expected, received $status"
	fi
	case "$scenario" in
	delivered)
		[[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 2 ]] || fail 'both casks must use the guarded finisher'
		[[ "$(grep -c '^signature$' "$state/signatures")" -eq 1 ]] || fail 'open Desktop handoff must verify the published signature'
		grep -q 'v7.213.2' "$state/summary" || fail 'delivery has no durable release identity'
		[[ ! -e "$state/gh-calls" ]] || fail 'a PR already titled for its release was retitled'
		;;
	stale-title)
		[[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 2 ]] || fail 'a retitled PR skipped the guarded finisher'
		grep -qxF 'api -X PATCH repos/devantler-tech/homebrew-tap/pulls/42 -f title=chore(cask): update ksail to v7.213.2' "$state/gh-calls" ||
			fail 'the CLI cask PR kept an earlier release title'
		grep -qxF 'api -X PATCH repos/devantler-tech/homebrew-tap/pulls/42 -f title=chore(cask): update ksail-desktop to v7.213.2' "$state/gh-calls" ||
			fail 'the Desktop cask PR kept an earlier release title'
		;;
	title-write-failed)
		[[ ! -e "$state/helper-calls" ]] || fail 'a PR whose title could not be set reached delivery'
		[[ "$(grep -c Failed "$state/summary")" -eq 2 ]] || fail 'a failed title update was not recorded as a failure'
		;;
	missing-labels)
		[[ "$(grep -c '^deliver$' "$state/helper-calls")" -eq 2 ]] || fail 'a relabelled PR skipped the guarded finisher'
		[[ "$(grep -cxF 'api -X POST repos/devantler-tech/homebrew-tap/issues/42/labels -f labels[]=dependencies' "$state/gh-calls")" -eq 2 ]] ||
			fail 'a cask PR missing the dependencies label was not given it'
		! grep -q 'labels\[\]=automation' "$state/gh-calls" || fail 'a label the PR already carries was added again'
		;;
	label-write-failed)
		[[ ! -e "$state/helper-calls" ]] || fail 'a PR whose labels could not be added reached delivery'
		[[ "$(grep -c Failed "$state/summary")" -eq 2 ]] || fail 'a failed label update was not recorded as a failure'
		;;
	merged)
		[[ "$(grep -c '^verify$' "$state/helper-calls")" -eq 2 ]] || fail 'finder candidates lack independent provenance verification'
		[[ ! -e "$state/gh-calls" ]] || fail 'a merged candidate was retitled'
		[[ -s "$state/signatures" ]] || fail 'merged Desktop delivery bypassed archive seal verification'
		;;
	merged-invalid-signature)
		[[ "$(grep -c '^verify$' "$state/helper-calls")" -eq 1 && -s "$state/signatures" ]] || fail 'invalid merged Desktop archive reached acceptance or blocked the CLI cask'
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
	ambiguous | rejected-current)
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
run_case repair-current 0
run_case merged-invalid-signature 1
run_case delivered 0
run_case stale-title 0
run_case title-write-failed 1
run_case missing-labels 0
run_case label-write-failed 1
run_case merged 0
run_case invalid-merge 1
run_case invalid-signature 1
run_case pending 75
run_case failed-check 1
run_case invalid-tag 1
run_case draft-release 1
run_case no-pr 75
run_case fork-pr 75
run_case wrong-ref 2 refs/heads/untrusted
