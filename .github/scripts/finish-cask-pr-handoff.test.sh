#!/usr/bin/env bash
# Exercise the real finisher, collector and validator; only GitHub transport and time are faked.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/bin"
fixture="${repo_root}/.github/fixtures/cask-pr-handoff/valid.json"

cat >"${work}/bin/date" <<'EOF'
#!/usr/bin/env bash
printf '2030-01-01T00:00:00Z\n'
EOF
cat >"${work}/bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"${work}/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
scenario="${FINISH_SCENARIO}"
state="${FINISH_STATE}"
fixture="${FINISH_FIXTURE}"
line="$*"
head=1111111111111111111111111111111111111111
base=3333333333333333333333333333333333333333
promoted=false
[[ -f "${state}/promoted" || "${scenario}" == ready-retry ]] && promoted=true
cursor=null
for arg in "$@"; do
	[[ "${arg}" == cursor=* ]] && cursor="${arg#cursor=}"
done
count() {
	local name="$1" n=0
	[[ ! -f "${state}/${name}" ]] || n="$(<"${state}/${name}")"
	n=$((n + 1))
	printf '%s\n' "${n}" >"${state}/${name}"
	printf '%s' "${n}"
}
pending_result() {
	jq -n --arg scenario "${scenario}" --arg phase "$1" '
	 {status:"pending",details:{message:"Merge request is in progress.",uuid:"630b9d5e-3f2a-4f7e-8b0c-2d5f9a8c1e42",
	 merge_method:"squash",merge_action:"direct_merge",expected_head_sha:"1111111111111111111111111111111111111111",bypass_rules:false}} |
	 if ($phase == "poll" and $scenario == "async-wrong-uuid") or ($phase == "initial" and $scenario == "async-initial-wrong-uuid") then .details.uuid="invalid/uuid"
	 elif $phase == "poll" and $scenario == "async-different-uuid" then .details.uuid="730b9d5e-3f2a-4f7e-8b0c-2d5f9a8c1e42"
	 elif ($phase == "poll" and $scenario == "async-wrong-head") or ($phase == "initial" and $scenario == "async-initial-wrong-head") then .details.expected_head_sha="2222222222222222222222222222222222222222"
	 elif ($phase == "poll" and $scenario == "async-wrong-method") or ($phase == "initial" and $scenario == "async-initial-wrong-method") then .details.merge_method="merge"
	 elif ($phase == "poll" and $scenario == "async-wrong-action") or ($phase == "initial" and $scenario == "async-initial-wrong-action") then .details.merge_action="merge_queue"
	 elif ($phase == "poll" and $scenario == "async-bypass") or ($phase == "initial" and $scenario == "async-initial-bypass") then .details.bypass_rules=true
	 elif $phase == "poll" and $scenario == "async-string-bypass" then .details.bypass_rules="false"
	 elif ($phase == "poll" and $scenario == "async-missing-options") or ($phase == "initial" and $scenario == "async-initial-missing-options") then del(.details.bypass_rules)
	 else . end'
}
case "${line}" in
*'enablePullRequestAutoMerge'*|*'--admin'*|*'--auto'*)
	printf 'unsafe mutation\n' >>"${state}/mutations"
	exit 2 ;;
*'mutation CaskPromote'*)
	printf 'promote\n' >>"${state}/mutations"
	[[ "${scenario}" != promotion-error ]] || exit 1
	touch "${state}/promoted"
	printf '%s\n' '{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}}}' ;;
*'api -X PUT repos/devantler-tech/homebrew-tap/pulls/42/merge-async '*)
	printf 'merge %s\n' "${line}" >>"${state}/mutations"
	[[ "${line}" == *"sha=${head}"* && "${line}" == *'merge_method=squash'* &&
	 "${line}" == *'merge_action=direct_merge'* && "${line}" == *'-F bypass_rules=false'* &&
	 "${line}" == *'X-GitHub-Api-Version: 2026-03-10'* ]] || exit 2
	[[ "${scenario}" != merge-rejected ]] || exit 1
	if [[ "${scenario}" == async-conflict ]]; then
		pending_result initial
		exit 1
	elif [[ "${scenario}" == async-* && "${scenario}" != async-already-merged ]]; then
		pending_result initial
	elif [[ "${scenario}" == merge-false ]]; then
		printf '%s\n' '{"status":"failed","details":{"message":"Merge rejected by rules."}}'
	else
		touch "${state}/merged"
		printf '%s\n' '{"status":"merged","details":{"message":"Pull request is merged.","sha":"4444444444444444444444444444444444444444"}}'
	fi ;;
*'api repos/devantler-tech/homebrew-tap/pulls/42/merge-async/630b9d5e-3f2a-4f7e-8b0c-2d5f9a8c1e42 '*)
	[[ "${line}" == *'X-GitHub-Api-Version: 2026-03-10'* ]] || exit 2
	n="$(count async-polls)"
	case "${scenario}" in
	async-lost-read) exit 1 ;;
	async-failed) printf '%s\n' '{"status":"failed","details":{"message":"Rules rejected the merge."}}' ;;
	async-enqueued) printf '%s\n' '{"status":"enqueued","details":{"message":"Added to a merge queue."}}' ;;
	async-unknown) printf '%s\n' '{"status":"accepted","details":{}}' ;;
	async-missing-details) printf '%s\n' '{"status":"pending"}' ;;
	async-malformed) printf 'not JSON\n' ;;
	async-final-wrong-head)
		touch "${state}/merged"
		printf '%s\n' '{"status":"merged","details":{"sha":"4444444444444444444444444444444444444444","expected_head_sha":"2222222222222222222222222222222222222222"}}' ;;
	async-final-wrong-uuid)
		touch "${state}/merged"
		printf '%s\n' '{"status":"merged","details":{"sha":"4444444444444444444444444444444444444444","uuid":"730b9d5e-3f2a-4f7e-8b0c-2d5f9a8c1e42"}}' ;;
	async-final-missing-sha) printf '%s\n' '{"status":"merged","details":{"message":"Merged"}}' ;;
	async-pending-success)
		if [[ "${n}" -eq 1 ]]; then pending_result poll
		else
			touch "${state}/merged"
			printf '%s\n' '{"status":"merged","details":{"sha":"4444444444444444444444444444444444444444"}}'
		fi ;;
	*) pending_result poll ;;
	esac ;;
*'api -X PUT repos/devantler-tech/homebrew-tap/pulls/42/merge '*)
	printf 'unsafe synchronous merge\n' >>"${state}/mutations"
	exit 2 ;;
*'query CaskState'*)
	n="$(count state-reads)"
	[[ "${scenario}" != state-read-error ]] || exit 1
	if [[ "${scenario}" == head-moved-before-promotion && "${n}" -ge 2 ]] ||
		[[ "${scenario}" == head-moved-after-promotion && "${promoted}" == true ]]; then
		head=2222222222222222222222222222222222222222
	fi
	jq -n --arg head "${head}" --arg base "${base}" --argjson ready "${promoted}" '
	 {data:{repository:{pullRequest:{number:42,id:"PR_42",state:"OPEN",isDraft:($ready|not),
	 headRefOid:$head,headRefName:"goreleaser/ksail",baseRefOid:$base,baseRefName:"main",
	 repository:{nameWithOwner:"devantler-tech/homebrew-tap"},headRepository:{nameWithOwner:"devantler-tech/homebrew-tap"},
	 author:{login:"devantler"},mergeable:"MERGEABLE",mergeStateStatus:"CLEAN",reviewDecision:null,
	 autoMergeRequest:null,commits:{nodes:[{commit:{oid:$head}}]}}}}}' |
	 jq --arg scenario "${scenario}" '
	 if $scenario == "auto-armed" then .data.repository.pullRequest.autoMergeRequest = {enabledAt:"2030-01-01T00:00:00Z"}
	 elif $scenario == "changes-requested" then .data.repository.pullRequest.reviewDecision = "CHANGES_REQUESTED"
	 elif $scenario == "conflict" then .data.repository.pullRequest.mergeable = "CONFLICTING"
	 elif $scenario == "postpromotion-blocked" and .data.repository.pullRequest.isDraft == false then .data.repository.pullRequest.mergeStateStatus = "BLOCKED"
	 elif $scenario == "graphql-errors" then .errors = [{message:"partial query"}]
	 else . end' ;;
*'query CaskChecks'*)
	[[ "${scenario}" != checks-read-error ]] || exit 1
	n="$(count "checks-${promoted}")"
	jq -n --arg head "${head}" --arg base "${base}" --argjson ready "${promoted}" '
	 def check($name;$required;$result;$time):
	 {__typename:"CheckRun",name:$name,isRequired:$required,status:"COMPLETED",conclusion:$result,startedAt:$time,
	 checkSuite:{commit:{oid:$head},app:{slug:"github-actions"},workflowRun:{event:"pull_request"}}};
	 {data:{repository:{pullRequest:{headRefOid:$head,baseRefOid:$base,commits:{nodes:[{commit:{oid:$head,statusCheckRollup:{contexts:{
	 totalCount:2,pageInfo:{hasNextPage:false,endCursor:null},nodes:[
	 check("CI - Required Checks";true;"SUCCESS";(if $ready then "2030-01-01T00:00:01Z" else "2029-01-01T00:00:00Z" end)),
	 check("🔍 Audit Casks";false;(if $ready then "SUCCESS" else "SKIPPED" end);(if $ready then "2030-01-01T00:00:01Z" else "2029-01-01T00:00:00Z" end))
	 ]}}}}]}}}}}' |
	 jq --arg scenario "${scenario}" --arg cursor "${cursor}" --argjson n "${n}" --argjson ready "${promoted}" '
	 .data.repository.pullRequest.commits.nodes[0].commit.statusCheckRollup.contexts |=
	 (if $scenario == "no-required" then .nodes |= map(.isRequired=false)
	 elif $scenario == "failed-required" then .nodes[0].conclusion="FAILURE"
	 elif $scenario == "missing-required-flag" then del(.nodes[0].isRequired)
	 elif $scenario == "pending-required" or ($scenario == "pending-then-pass" and ($ready|not) and $n == 1) then .nodes[0].status="IN_PROGRESS" | .nodes[0].conclusion=null
	 elif $scenario == "missing-audit" and $ready then .nodes[1].conclusion="SKIPPED"
	 elif $scenario == "stale-audit" and $ready then .nodes[1].startedAt="2029-01-01T00:00:00Z"
	 elif $scenario == "wrong-check-commit" then .nodes[0].checkSuite.commit.oid="2222222222222222222222222222222222222222"
	 elif $scenario == "stale-aggregate" and $ready then .nodes[0].startedAt="2029-01-01T00:00:00Z"
	 elif $scenario == "audit-then-pass" and $ready and $n == 1 then .nodes[1].status="IN_PROGRESS" | .nodes[1].conclusion=null
	 elif $scenario == "partial-checks" then .totalCount=3
	 elif $scenario == "paginated-success" then
	   if $cursor == "next" then .nodes=[.nodes[1]]
	   else .nodes=[.nodes[0]] | .pageInfo={hasNextPage:true,endCursor:"next"} end
	 elif $scenario == "later-page-failure" then
	   if $cursor == "next" then .nodes=[(.nodes[0]|.name="later required"|.conclusion="FAILURE")] | .totalCount=2
	   else .nodes=[.nodes[0]] | .totalCount=2 | .pageInfo={hasNextPage:true,endCursor:"next"} end
	 elif $scenario == "cursor-error" then .pageInfo={hasNextPage:true,endCursor:null}
	 else . end) |
	 if $scenario == "status-commit-mismatch" then .data.repository.pullRequest.commits.nodes[0].commit.oid="2222222222222222222222222222222222222222" else . end' ;;
*'query CaskThreads'*|*'query CaskReviews'*)
	connection=reviewThreads
	[[ "${line}" != *'query CaskReviews'* ]] || connection=latestReviews
	jq -n --arg head "${head}" --arg base "${base}" --arg connection "${connection}" --arg scenario "${scenario}" --arg cursor "${cursor}" '
	 {data:{repository:{pullRequest:{headRefOid:$head,baseRefOid:$base,($connection):{
	 totalCount:0,nodes:[],pageInfo:{hasNextPage:false,endCursor:null}}}}}} |
	 if ($scenario == "later-unresolved" and $connection == "reviewThreads") or ($scenario == "later-negative-review" and $connection == "latestReviews") then
	   .data.repository.pullRequest[$connection] |=
	   (if $cursor == "next" then .totalCount=1 | .nodes=[(if $connection == "reviewThreads" then {isResolved:false} else {state:"CHANGES_REQUESTED"} end)]
	    else .totalCount=1 | .pageInfo={hasNextPage:true,endCursor:"next"} end)
	 elif $scenario == "paginated-success" then .data.repository.pullRequest[$connection] |=
	   (if $cursor == "next" then .totalCount=1 | .nodes=[(if $connection == "reviewThreads" then {isResolved:true} else {state:"APPROVED"} end)]
	    else .totalCount=1 | .pageInfo={hasNextPage:true,endCursor:"next"} end)
	 elif $scenario == "partial-threads" and $connection == "reviewThreads" then .data.repository.pullRequest[$connection].totalCount=1
	 else . end' ;;
'api repos/devantler-tech/homebrew-tap/pulls/42')
	if [[ -f "${state}/merged" ]]; then
		[[ "${scenario}" != readback-error ]] || exit 1
		jq '.pr | .state="closed" | .draft=false | .merged=true | .merge_commit_sha="4444444444444444444444444444444444444444" | .merged_at="2030-01-01T00:00:02Z"' "${fixture}" |
		 jq --arg scenario "${scenario}" 'if $scenario == "readback-wrong-head" then .head.sha="2222222222222222222222222222222222222222" else . end'
	else
		n="$(count rest-reads)"
		jq --argjson ready "${promoted}" --argjson n "${n}" --arg scenario "${scenario}" '.pr | .node_id="PR_42" | .draft=($ready|not) | .base.sha="3333333333333333333333333333333333333333" |
		 if $scenario == "head-moved-during-collection" and $n > 1 then .head.sha="2222222222222222222222222222222222222222" else . end' "${fixture}"
	fi ;;
'api --paginate --slurp repos/devantler-tech/homebrew-tap/pulls/42/files?per_page=100')
	jq '[.files]' "${fixture}" ;;
'api --paginate --slurp repos/devantler-tech/homebrew-tap/labels?per_page=100')
	jq '[.availableLabels]' "${fixture}" ;;
'api repos/devantler-tech/ksail/releases/tags/v7.166.1')
	[[ "${scenario}" != release-read-error ]] || exit 1
	n="$(count release-reads)"
	jq '{tag_name:"v7.166.1",draft:false,published_at:"2029-01-01T00:00:00Z",assets:.releaseAssets}' "${fixture}" |
	 jq --arg scenario "${scenario}" --argjson ready "${promoted}" --argjson n "${n}" '
	 if $scenario == "draft-release" then .draft=true
	 elif $scenario == "unpublished-release" then .published_at=null
	 elif $scenario == "wrong-release-tag" then .tag_name="v7.166.2"
	 elif $scenario == "digest-mismatch" or ($scenario == "digest-changed-after-promotion" and $ready) or ($scenario == "final-release-digest-race" and $n >= 7) then .assets[0].digest="sha256:1111111111111111111111111111111111111111111111111111111111111111"
	 elif $scenario == "release-changed-after-promotion" and $ready then .draft=true
	 else . end' ;;
'api repos/devantler-tech/homebrew-tap/contents/Casks/ksail.rb?ref=1111111111111111111111111111111111111111')
	jq '.headFile' "${fixture}" ;;
'api --paginate --slurp repos/devantler-tech/homebrew-tap/pulls/42/commits?per_page=100')
	jq -n --arg head "${head}" --arg scenario "${scenario}" '
	 [[{sha:$head,author:{login:"goreleaserbot"},committer:{login:"goreleaserbot"},commit:{
	 author:{name:"goreleaserbot",email:"bot@goreleaser.com"},committer:{name:"goreleaserbot",email:"bot@goreleaser.com"},
	 message:(if $scenario == "adaptation-commit" then "fix: hand edit cask" else "chore(cask): update ksail to v7.166.1" end)}}]]' ;;
*) printf 'unexpected transport request: %s\n' "${line}" >&2; exit 2 ;;
esac
EOF
chmod +x "${work}/bin/gh" "${work}/bin/date" "${work}/bin/sleep"

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	exit 1
}
run_case() {
	local scenario="$1" success="$2" promotes="$3" merges="$4" code=0
	local state="${work}/${scenario}"
	mkdir -p "${state}"
	: >"${state}/mutations"
	PATH="${work}/bin:${PATH}" FINISH_FIXTURE="${fixture}" FINISH_STATE="${state}" FINISH_SCENARIO="${scenario}" \
		bash "${script_dir}/finish-cask-pr-handoff.sh" \
		--tap devantler-tech/homebrew-tap --pr 42 --cask-name ksail --source-repo devantler-tech/ksail \
		--tag v7.166.1 --attempts 2 --interval 0 >"${state}/output" 2>&1 || code=$?
	if [[ "${success}" == true && "${code}" -ne 0 ]] || [[ "${success}" == false && "${code}" -eq 0 ]]; then
		cat "${state}/output" >&2
		fail "${scenario}: expected success=${success}, exit=${code}"
	fi
	if [[ "$(grep -c '^promote$' "${state}/mutations" || true)" -ne "${promotes}" ]]; then
		cat "${state}/output" >&2
		fail "${scenario}: wrong promotion count"
	fi
	if [[ "$(grep -c '^merge ' "${state}/mutations" || true)" -ne "${merges}" ]]; then
		cat "${state}/output" >&2
		fail "${scenario}: wrong direct merge count"
	fi
	if [[ "${success}" == true ]]; then
		grep -q '1111111111111111111111111111111111111111' "${state}/output" || fail "${scenario}: verified head missing from delivery result"
		[[ -f "${state}/merged" ]] || fail "${scenario}: readiness incorrectly counted as delivered"
	fi
	if [[ "${scenario}" == async-timeout || "${scenario}" == async-pending-success ]]; then
		[[ "$(<"${state}/async-polls")" -eq 2 ]] || fail "${scenario}: result polling did not honor its independent bound"
	fi
	if [[ "${scenario}" == async-conflict || "${scenario}" == async-initial-* ]]; then
		[[ ! -f "${state}/async-polls" ]] || fail "${scenario}: adopted an unverified request"
	fi
	printf 'PASS: %s\n' "${scenario}"
}

# Removing the direct merge, SHA pin, post-promotion audit, or immutable readback breaks these.
run_case draft-success true 1 1
run_case ready-retry true 0 1
run_case pending-then-pass true 1 1
run_case audit-then-pass true 1 1
run_case paginated-success true 1 1
run_case admin-capable true 1 1
run_case async-pending-success true 1 1
run_case async-already-merged true 1 1
for scenario in draft-release unpublished-release wrong-release-tag release-read-error digest-mismatch \
	no-required failed-required missing-required-flag pending-required state-read-error checks-read-error \
	graphql-errors partial-checks later-page-failure cursor-error later-unresolved later-negative-review \
	partial-threads changes-requested auto-armed conflict adaptation-commit head-moved-during-collection \
	head-moved-before-promotion wrong-check-commit status-commit-mismatch; do
	run_case "${scenario}" false 0 0
done
for scenario in missing-audit stale-aggregate stale-audit head-moved-after-promotion promotion-error \
	postpromotion-blocked digest-changed-after-promotion release-changed-after-promotion final-release-digest-race; do
	run_case "${scenario}" false 1 0
done
for scenario in merge-rejected merge-false readback-error readback-wrong-head; do
	run_case "${scenario}" false 1 1
done
for scenario in async-conflict async-failed async-enqueued async-unknown async-missing-details async-malformed \
	async-lost-read async-timeout async-wrong-uuid async-different-uuid async-wrong-head async-wrong-method \
	async-wrong-action async-bypass async-string-bypass async-missing-options async-initial-wrong-uuid \
	async-initial-wrong-head async-initial-wrong-method async-initial-wrong-action async-initial-bypass \
	async-initial-missing-options async-final-wrong-head async-final-wrong-uuid async-final-missing-sha; do
	run_case "${scenario}" false 1 1
done

printf 'All exact-head cask delivery behavior tests passed.\n'
