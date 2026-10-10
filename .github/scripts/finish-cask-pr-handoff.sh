#!/usr/bin/env bash
# Complete a generated cask release by a direct, verified-head squash merge.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
tap="" pr="" name="" tag="" source_repo="devantler-tech/ksail"
attempts=30 interval=10
# Describe the fixed release identity and bounded wait arguments accepted by this command.
usage() {
	printf 'Usage: finish-cask-pr-handoff.sh --tap OWNER/REPO --pr NUMBER --cask-name NAME --tag TAG [--source-repo OWNER/REPO] [--attempts 1..30] [--interval 0..10]\n' >&2
}
while (($#)); do
	case "$1" in
	--tap | --pr | --cask-name | --tag | --source-repo | --attempts | --interval)
		[[ $# -ge 2 ]] || {
			usage
			exit 2
		}
		case "$1" in
		--tap) tap="$2" ;; --pr) pr="$2" ;; --cask-name) name="$2" ;; --tag) tag="$2" ;;
		--source-repo) source_repo="$2" ;; --attempts) attempts="$2" ;; --interval) interval="$2" ;;
		esac
		shift 2
		;;
	*)
		usage
		exit 2
		;;
	esac
done
if [[ "${tap}" != devantler-tech/homebrew-tap || "${source_repo}" != devantler-tech/ksail ||
	! "${pr}" =~ ^[1-9][0-9]*$ || ! "${name}" =~ ^ksail(-desktop)?$ ||
	! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ||
	! "${attempts}" =~ ^([1-9]|[12][0-9]|30)$ || ! "${interval}" =~ ^([0-9]|10)$ ]]; then
	usage
	exit 2
fi
work="$(mktemp -d)"
handoff_delivered=false
# Remove only this invocation's temporary evidence directory on exit.
cleanup() {
	local code=$?
	trap - EXIT
	rm -rf "${work}" || code=1
	# Bash 3.2 can enter EXIT with status zero after a nounset expansion abort.
	# Cleanup may report success only after the exact merged readback completed.
	if [[ "${handoff_delivered}" != true && "${code}" -eq 0 ]]; then
		printf 'BLOCKED: handoff ended without verified delivery\n' >&2
		code=1
	fi
	exit "${code}"
}
trap cleanup EXIT
head="" base="" draft="" node="" boundary="" merge_state=""
# Report a refusal without converting missing or invalid evidence into delivery success.
blocked() { printf 'BLOCKED: %s\n' "$1" >&2; }

# Require the requested source tag to identify a published, non-draft release.
published_release() {
	if ! gh api "repos/${source_repo}/releases/tags/${tag}" >"${work}/release.json"; then
		blocked 'release read failed; publication is unknown'
		return 1
	fi
	if ! jq -e --arg tag "${tag}" --slurpfile evidence "${work}/evidence.json" '
	 type == "object" and .tag_name == $tag and .draft == false
	 and (.published_at | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T"))
	 and (.assets | type == "array" and length > 0)
	 and ([.assets[] | {name,digest}] | sort_by(.name)) == ($evidence[0].releaseAssets | sort_by(.name))
	' "${work}/release.json" >/dev/null; then
		blocked 'release publication or asset digest evidence changed'
		return 1
	fi
}

# The programmed no-review path is deliberately narrower than merely a trusted PR author.
# These identities/messages mirror the canonical monorepo programmed-bot exemption's cask arm.
# Admit only the programmed cask updater's expected author and commit sequence.
provenance() {
	if ! gh api --paginate --slurp "repos/${tap}/pulls/${pr}/commits?per_page=100" >"${work}/commits.json"; then
		blocked 'commit provenance read failed'
		return 1
	fi
	if ! jq -e --arg head "${head}" --arg component "${name}" --arg tag "${tag}" '
	 def generator:
	   .author.login == "goreleaserbot" and .committer.login == "goreleaserbot"
	   and .commit.author.name == "goreleaserbot" and .commit.author.email == "bot@goreleaser.com"
	   and .commit.committer.name == "goreleaserbot" and .commit.committer.email == "bot@goreleaser.com"
	   and (.commit.message | test("^(Brew cask update for \($component) version |chore\\(cask\\): update \($component) to )v[0-9]+\\.[0-9]+\\.[0-9]+([+-][0-9A-Za-z.-]+)?$"));
	 def style($login;$identity;$email;$message):
	   (.author.login // "") == $login and (.committer.login // "") == $login
	   and .commit.author.name == $identity and .commit.author.email == $email
	   and .commit.committer.name == $identity and .commit.committer.email == $email
	   and .commit.message == $message;
	 type == "array" and all(.[]; type == "array") and ([.[][]] |
	   length > 0 and (.[0] | generator) and .[-1].sha == $head
	   and all(.[]; generator
	     or style("";"generator-bot";"generator-bot@users.noreply.github.com";"style: autocorrect Casks (brew style --fix)")
	     or style("github-actions[bot]";"github-actions[bot]";"41898282+github-actions[bot]@users.noreply.github.com";"style: brew style --fix generated cask"))
	   and any(.[]; generator and (.commit.message == "Brew cask update for \($component) version \($tag)"
	     or .commit.message == "chore(cask): update \($component) to \($tag)")))
	' "${work}/commits.json" >/dev/null; then
		blocked 'current-head commits are not the programmed cask release shape'
		return 1
	fi
}

# Verify the captured PR identity and metadata still describe the same head and base.
rebind_rest() {
	if ! gh api "repos/${tap}/pulls/${pr}" >"${work}/rebound-pr.json"; then
		blocked 'PR rebind read failed'
		return 1
	fi
	if ! jq -e --slurpfile prior "${work}/evidence.json" '
	 def identity: [.state,.draft,.user.login,.head.sha,.head.ref,.head.repo.full_name,
	   .base.sha,.base.ref,.title,(.labels|map(.name)|sort)];
	 identity == ($prior[0].pr | identity)
	' "${work}/rebound-pr.json" >/dev/null; then
		blocked 'PR changed while collecting or rebinding its evidence'
		return 1
	fi
}

# Collect and validate release contents, then capture the identity used by every later gate.
capture() {
	if ! "${script_dir}/collect-cask-pr-handoff.sh" --tap "${tap}" --pr "${pr}" \
		--source-repo "${source_repo}" --tag "${tag}" --output "${work}/evidence.json"; then
		blocked 'cask handoff collection failed'
		return 1
	fi
	local captured_head captured_base captured_draft
	if ! captured_head="$(jq -er '.pr.head.sha | select(test("^[0-9a-f]{40}$"))' "${work}/evidence.json")" ||
		! captured_base="$(jq -er '.pr.base.sha | select(test("^[0-9a-f]{40}$"))' "${work}/evidence.json")" ||
		! captured_draft="$(jq -er '.pr.draft | if type == "boolean" then tostring else error("draft missing") end' "${work}/evidence.json")"; then
		blocked 'head, base, or draft evidence is incomplete'
		return 1
	fi
	if [[ -n "${head}" && ("${captured_head}" != "${head}" || "${captured_base}" != "${base}" || "${captured_draft}" != "${draft}") ]]; then
		blocked 'verified head, base, or draft state moved; reevaluation is required'
		return 1
	fi
	head="${captured_head}" base="${captured_base}" draft="${captured_draft}"
	local args=(--evidence "${work}/evidence.json" --tap "${tap}" --cask-name "${name}" --tag "${tag}" --source-repo "${source_repo}" --prepared)
	[[ "${draft}" == true ]] || args+=(--ready)
	if ! "${script_dir}/validate-cask-pr-handoff.sh" "${args[@]}" || ! published_release || ! provenance || ! rebind_rest; then
		return 1
	fi
}

# Fetch one readiness page and reject API failures, GraphQL errors, or a missing PR.
graphql() {
	local query="$1" cursor="${2:-null}"
	# Keep the array nonempty: Bash 3.2 treats an empty array expansion as unset
	# under nounset even when the array has been explicitly initialized.
	local gql_args=(-f "query=${query}" -F "owner=${tap%%/*}" -F "name=${tap#*/}" -F "number=${pr}")
	if [[ "${query}" == *"\$cursor"* ]]; then
		if [[ "${cursor}" == null ]]; then
			gql_args+=(-F cursor=null)
		else
			gql_args+=(-f "cursor=${cursor}")
		fi
	fi
	if ! gh api graphql "${gql_args[@]}" >"${work}/graphql.json"; then
		blocked 'GraphQL read failed; readiness is unknown'
		return 1
	fi
	if ! jq -e '(.errors == null or .errors == []) and (.data.repository.pullRequest | type == "object")' \
		"${work}/graphql.json" >/dev/null; then
		blocked 'GraphQL returned incomplete or erroneous evidence'
		return 1
	fi
}

# shellcheck disable=SC2016 # These are GraphQL variables, not shell interpolation.
state_query='query CaskState($owner:String!,$name:String!,$number:Int!) {
 repository(owner:$owner,name:$name) { pullRequest(number:$number) {
 number id state isDraft headRefOid headRefName baseRefOid baseRefName
 repository { nameWithOwner } headRepository { nameWithOwner } author { login }
 mergeable mergeStateStatus reviewDecision autoMergeRequest { enabledAt }
 commits(last:1) { nodes { commit { oid } } }
 } } }'
# Recheck exact-head mergeability, review state, and the absence of armed auto-merge.
read_state() {
	if ! graphql "${state_query}"; then return 1; fi
	if ! jq -e --arg head "${head}" --arg base "${base}" --arg tap "${tap}" --arg branch "goreleaser/${name}" --argjson pr "${pr}" '
	 .data.repository.pullRequest |
	 .number == $pr and .state == "OPEN" and (.isDraft | type == "boolean")
	 and (.id | type == "string" and length > 0) and .headRefOid == $head and .baseRefOid == $base
	 and .headRefName == $branch and .baseRefName == "main"
	 and .repository.nameWithOwner == $tap and .headRepository.nameWithOwner == $tap and .author.login == "devantler"
	 and (.commits.nodes | length == 1) and .commits.nodes[0].commit.oid == $head
	 and has("autoMergeRequest") and .autoMergeRequest == null and has("reviewDecision")
	 and .reviewDecision != "CHANGES_REQUESTED"
	' "${work}/graphql.json" >/dev/null; then
		blocked 'PR identity, head/base, review, or unarmed state changed'
		return 1
	fi
	if ! jq -e '.data.repository.pullRequest.mergeable == "MERGEABLE"' "${work}/graphql.json" >/dev/null; then
		blocked 'PR is not demonstrably mergeable'
		return 1
	fi
	if [[ "${draft}" == true ]]; then
		if ! jq -e '.data.repository.pullRequest.isDraft == true' "${work}/graphql.json" >/dev/null; then return 1; fi
	else
		if ! jq -e '.data.repository.pullRequest.isDraft == false' "${work}/graphql.json" >/dev/null; then return 1; fi
	fi
	if ! node="$(jq -er '.data.repository.pullRequest.id' "${work}/graphql.json")" ||
		! merge_state="$(jq -er '.data.repository.pullRequest.mergeStateStatus | select(type == "string")' "${work}/graphql.json")"; then return 1; fi
}

# shellcheck disable=SC2016 # Literal GraphQL variables.
checks_query='query CaskChecks($owner:String!,$name:String!,$number:Int!,$cursor:String) {
 repository(owner:$owner,name:$name) { pullRequest(number:$number) { headRefOid baseRefOid
 commits(last:1) { nodes { commit { oid statusCheckRollup { contexts(first:100,after:$cursor) {
 totalCount pageInfo { hasNextPage endCursor } nodes { __typename
 ... on CheckRun { name status conclusion startedAt isRequired(pullRequestNumber:$number)
 checkSuite { commit { oid } app { id slug } workflowRun { id runNumber event workflow { id } } } }
 ... on StatusContext { context state createdAt isRequired(pullRequestNumber:$number) }
 } } } } } } } } }'
# shellcheck disable=SC2016 # Literal GraphQL variables.
threads_query='query CaskThreads($owner:String!,$name:String!,$number:Int!,$cursor:String) {
 repository(owner:$owner,name:$name) { pullRequest(number:$number) { headRefOid baseRefOid
 reviewThreads(first:100,after:$cursor) { totalCount pageInfo { hasNextPage endCursor } nodes { isResolved } }
 } } }'
# shellcheck disable=SC2016 # Literal GraphQL variables.
reviews_query='query CaskReviews($owner:String!,$name:String!,$number:Int!,$cursor:String) {
 repository(owner:$owner,name:$name) { pullRequest(number:$number) { headRefOid baseRefOid
 latestReviews(first:100,after:$cursor) { totalCount pageInfo { hasNextPage endCursor } nodes { state } }
 } } }'

# Join bounded, complete readiness pages while refusing head, base, total, or cursor drift.
connection() {
	local kind="$1" query path cursor=null seen='|' total=-1 actual page
	case "${kind}" in
	checks)
		query="${checks_query}"
		path='.commits.nodes[0].commit.statusCheckRollup.contexts'
		;;
	threads)
		query="${threads_query}"
		path='.reviewThreads'
		;;
	reviews)
		query="${reviews_query}"
		path='.latestReviews'
		;;
	esac
	printf '[]\n' >"${work}/${kind}.json"
	for ((page = 0; page < 20; page++)); do
		if ! graphql "${query}" "${cursor}"; then return 1; fi
		if ! jq -e --arg head "${head}" --arg base "${base}" '
		 .data.repository.pullRequest | .headRefOid == $head and .baseRefOid == $base
		' "${work}/graphql.json" >/dev/null; then
			blocked 'head or base moved during readiness pagination'
			return 1
		fi
		if [[ "${kind}" == checks ]] && ! jq -e --arg head "${head}" '
		 .data.repository.pullRequest.commits.nodes | length == 1 and .[0].commit.oid == $head
		' "${work}/graphql.json" >/dev/null; then
			blocked 'check rollup is not bound to the verified commit'
			return 1
		fi
		if ! jq ".data.repository.pullRequest${path}" "${work}/graphql.json" >"${work}/page.json" ||
			! jq -e 'type == "object" and (.nodes | type == "array")
			 and (.totalCount | type == "number" and . >= 0 and . == floor)
			 and (.pageInfo.hasNextPage | type == "boolean")
			 and (if .pageInfo.hasNextPage then (.pageInfo.endCursor | type == "string" and length > 0) else true end)
			' "${work}/page.json" >/dev/null; then
			blocked 'readiness pagination is incomplete'
			return 1
		fi
		if ! actual="$(jq -er '.totalCount' "${work}/page.json")"; then return 1; fi
		[[ "${total}" -eq -1 ]] && total="${actual}"
		if [[ "${actual}" -ne "${total}" ]]; then
			blocked 'readiness changed during pagination'
			return 1
		fi
		if ! jq -s '.[0] + .[1].nodes' "${work}/${kind}.json" "${work}/page.json" >"${work}/joined.json"; then return 1; fi
		mv "${work}/joined.json" "${work}/${kind}.json"
		if jq -e '.pageInfo.hasNextPage == false' "${work}/page.json" >/dev/null; then
			if ! jq -e --argjson total "${total}" 'length == $total' "${work}/${kind}.json" >/dev/null; then
				blocked 'readiness page count does not match complete evidence'
				return 1
			fi
			return 0
		fi
		if ! cursor="$(jq -er '.pageInfo.endCursor' "${work}/page.json")"; then return 1; fi
		if [[ "${seen}" == *"|${cursor}|"* ]]; then
			blocked 'readiness pagination cursor repeated'
			return 1
		fi
		seen+="${cursor}|"
	done
	blocked 'readiness pagination exceeded its bound'
	return 1
}

# Return success only for complete review/check evidence; return 3 while valid checks settle.
screen() {
	if ! read_state || ! connection checks || ! connection threads || ! connection reviews; then return 1; fi
	if ! jq -e 'all(.[]; (.isResolved | type == "boolean") and .isResolved == true)' "${work}/threads.json" >/dev/null ||
		! jq -e 'all(.[]; .state as $state | (["APPROVED","COMMENTED","DISMISSED"] | index($state)) != null)' "${work}/reviews.json" >/dev/null; then
		blocked 'unresolved threads or negative/incomplete review evidence'
		return 1
	fi
	if ! jq -e --arg head "${head}" '
	 all(.[]; (.isRequired | type == "boolean") and
	   (if .__typename == "CheckRun" then (.name | type == "string" and length > 0) and (.status | type == "string") and .checkSuite.commit.oid == $head
	    elif .__typename == "StatusContext" then (.context | type == "string" and length > 0) and (.state | type == "string")
	    else false end))
	' "${work}/checks.json" >/dev/null; then
		blocked 'check metadata is malformed or incompletely observed'
		return 1
	fi
	# The rollup contains earlier executions at this same commit. Select a newer execution
	# only inside its exact App/workflow/event identity; different analysis workflows must
	# never clear one another. Ambiguous same-run results remain independently blocking.
	if ! jq '
	 def text: type == "string" and length > 0;
	 def numbered: type == "number" and . > 0 and . == floor;
	 def workflow: .__typename == "CheckRun" and .checkSuite.workflowRun != null;
	 if all(.[]; .__typename != "CheckRun" or
	   ((.checkSuite.app.id | text) and (.checkSuite.app.slug | text) and
	    (if workflow then (.checkSuite.workflowRun.id | text) and
	      (.checkSuite.workflowRun.runNumber | numbered) and
	      (.checkSuite.workflowRun.event | text) and (.checkSuite.workflowRun.workflow.id | text)
	     else .checkSuite.app.slug != "github-actions" end))) then
	   [.[] | select(workflow | not)] as $independent |
	   ([.[] | select(workflow)] |
	    group_by([.checkSuite.app.id,.checkSuite.workflowRun.workflow.id,.checkSuite.workflowRun.event]) |
	    map(. as $history |
	      (map(.checkSuite.workflowRun.runNumber) | max) as $latest |
	      map(select(.checkSuite.workflowRun.runNumber == $latest)) as $current |
	      if ($current | map(.checkSuite.workflowRun.id) | unique | length) != 1 then
	        error("ambiguous current workflow execution")
	      elif all($history[]; . as $previous | .isRequired != true or
	        all($current[]; .name != $previous.name or .isRequired == true)) then
	        {checks:$current, pendingRequired:any($history[]; . as $previous |
	          .isRequired == true and all($current[]; .name != $previous.name))}
	      else error("current execution withdrew a required check") end)) |
	   {checks:($independent + (map(.checks) | add // [])),
	    pendingRequired:any(.[]; .pendingRequired)}
	 else error("check execution identity is incomplete") end
	' "${work}/checks.json" >"${work}/current-checks.json"; then
		blocked 'current check executions are absent, ambiguous or incompletely observed'
		return 1
	fi
	if ! jq '.checks' "${work}/current-checks.json" >"${work}/checks.json"; then return 1; fi
	if jq -e 'any(.[];
	 if .__typename == "CheckRun" then .status == "COMPLETED" and (.conclusion != "SUCCESS" and .conclusion != "NEUTRAL" and .conclusion != "SKIPPED")
	 else .state != "SUCCESS" and .state != "PENDING" end)' "${work}/checks.json" >/dev/null; then
		blocked 'a current-head check failed'
		return 1
	fi
	# GitHub creates the dependent required aggregate after its prerequisite jobs.
	# A complete read can therefore contain no required check yet. Keep the draft
	# pending inside the existing bound; never promote until a real required check
	# has appeared and settled. Failed reads and malformed metadata remain terminal.
	if ! jq -e 'length > 0 and any(.[]; .isRequired == true)' "${work}/checks.json" >/dev/null; then
		return 3
	fi
	# A dependent aggregate may not exist yet in a newly started workflow. Keep this
	# observation pending without inventing a check result or ignoring a current failure.
	if jq -e '.pendingRequired == true' "${work}/current-checks.json" >/dev/null; then return 3; fi
	if ! jq -e 'all(.[]; if .__typename == "CheckRun" then .status == "COMPLETED" else .state == "SUCCESS" end)' "${work}/checks.json" >/dev/null; then
		return 3
	fi
	if [[ "${draft}" == false ]]; then
		# Draft CI skips the online audit and can still publish a green aggregate. Promotion must
		# be followed by actual successful audit/aggregate runs, never that earlier draft result.
		if ! jq -e --arg head "${head}" --arg boundary "${boundary}" '
		 def fresh($name): any(.[]; .__typename == "CheckRun" and .name == $name
		   and .status == "COMPLETED" and .conclusion == "SUCCESS"
		   and .checkSuite.app.slug == "github-actions" and .checkSuite.workflowRun.event == "pull_request"
		   and .checkSuite.commit.oid == $head and (.startedAt | type == "string")
		   and ($boundary == "" or ((.startedAt | sub("\\.[0-9]+Z$";"Z")) >= $boundary)));
		 fresh("🔍 Audit Casks") and fresh("CI - Required Checks")
		' "${work}/checks.json" >/dev/null; then return 3; fi
		if [[ "${merge_state}" != CLEAN ]]; then return 3; fi
	fi
	return 0
}

# Give each readiness phase its own wait budget; terminal refusals never consume retry loops.
wait_screen() {
	local code remaining="${attempts}"
	while ((remaining > 0)); do
		code=0
		screen || code=$?
		case "${code}" in
		0) return 0 ;;
		3)
			remaining=$((remaining - 1))
			if ((remaining > 0)); then sleep "${interval}"; fi
			;;
		*) return 1 ;;
		esac
	done
	blocked 'required checks or the actual online audit did not settle within the wait bound'
	return 1
}

capture
wait_screen
capture
read_state
if [[ "${draft}" == true ]]; then
	boundary="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	if [[ ! "${boundary}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
		blocked 'promotion time boundary is unknown'
		exit 1
	fi
	# shellcheck disable=SC2016 # Literal GraphQL variable.
	mutation='mutation CaskPromote($id:ID!) { markPullRequestReadyForReview(input:{pullRequestId:$id}) { pullRequest { isDraft } } }'
	if ! gh api graphql -f "query=${mutation}" -F id="${node}" >"${work}/promotion.json" ||
		! jq -e '(.errors == null or .errors == []) and .data.markPullRequestReadyForReview.pullRequest.isDraft == false' "${work}/promotion.json" >/dev/null; then
		blocked 'promotion did not return a confirmed ready PR'
		exit 1
	fi
	draft=false
fi
wait_screen
capture
if ! screen || ! read_state || [[ "${merge_state}" != CLEAN ]] || ! rebind_rest || ! published_release; then exit 1; fi

# The async API provides the explicit non-bypass contract the synchronous merge endpoint lacks
# for privileged actors. Accepted requests are not delivery; only the merged result and exact
# PR readback can complete this handoff. https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously
merge_sha="" merge_uuid=""
# Confirm the asynchronous request actually merged the captured head before reporting delivery.
merge_result() {
	local endpoint="repos/${tap}/pulls/${pr}/merge-async" status polls=0
	if ! gh api -X PUT "${endpoint}" \
		-H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2026-03-10' \
		-f "sha=${head}" -f merge_method=squash -f merge_action=direct_merge -F bypass_rules=false \
		>"${work}/merge.json"; then
		# In particular, HTTP 409 belongs to an existing request: never adopt its UUID or options.
		blocked 'non-bypassing pinned merge request was rejected; immutable merged reconciliation is required'
		return 1
	fi
	while true; do
		if ! status="$(jq -er 'select(type == "object" and (.details | type == "object")) | .status | select(type == "string")' "${work}/merge.json")"; then
			blocked 'asynchronous merge returned malformed or incomplete evidence'
			return 1
		fi
		# Pending results require all options. Completed results may omit these fields, but any
		# supplied metadata must still identify this request and its verified, non-bypassing head.
		if ! jq -e --arg uuid "${merge_uuid}" --arg head "${head}" '
      .status as $status | .details |
      def supplied($key;$expected): if has($key) then .[$key] == $expected else $status != "pending" end;
      (if has("uuid") then
        (.uuid | type == "string" and test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"))
        and ($uuid == "" or .uuid == $uuid)
       else $status != "pending" end)
      and supplied("expected_head_sha";$head) and supplied("merge_method";"squash")
      and supplied("merge_action";"direct_merge") and supplied("bypass_rules";false)
    ' "${work}/merge.json" >/dev/null; then
			blocked 'asynchronous merge request UUID, head, or non-bypass options do not match'
			return 1
		fi
		case "${status}" in
		merged)
			if ! merge_sha="$(jq -er '.details.sha | select(type == "string" and test("^[0-9a-f]{40}$"))' "${work}/merge.json")"; then
				blocked 'merged result has no verified merge SHA'
				return 1
			fi
			return 0
			;;
		pending)
			if [[ -z "${merge_uuid}" ]]; then
				if ! merge_uuid="$(jq -er '.details.uuid' "${work}/merge.json")"; then return 1; fi
			fi
			if ((polls >= attempts)); then
				blocked 'asynchronous merge result did not settle within its independent poll bound'
				return 1
			fi
			if ((polls > 0)); then sleep "${interval}"; fi
			if ! gh api "${endpoint}/${merge_uuid}" \
				-H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2026-03-10' \
				>"${work}/merge.json"; then
				blocked 'asynchronous merge result read failed; delivery is unknown'
				return 1
			fi
			polls=$((polls + 1))
			;;
		*)
			blocked 'asynchronous request failed, enqueued, or returned an unknown terminal status'
			return 1
			;;
		esac
	done
}
merge_result
if ! gh api "repos/${tap}/pulls/${pr}" >"${work}/merged-pr.json" ||
	! jq -e --arg head "${head}" --arg sha "${merge_sha}" '
	 .state == "closed" and .merged == true and .draft == false
	 and .head.sha == $head and .merge_commit_sha == $sha
	 and (.merged_at | type == "string" and length > 0)
	' "${work}/merged-pr.json" >/dev/null; then
	blocked 'merge response/readback is incomplete; immutable merged reconciliation is required'
	exit 1
fi
handoff_delivered=true
printf 'PASS: %s#%s cask %s merged for %s at verified head %s (merge %s)\n' "${tap}" "${pr}" "${name}" "${tag}" "${head}" "${merge_sha}"
