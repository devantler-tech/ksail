#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  redraft-evergreen-cask-prs.sh --tap OWNER/REPO CASK_NAME [CASK_NAME...]

Pre-release checkpoint: demote any reused open evergreen cask PR back to draft
BEFORE GoReleaser rewrites its branch, so an auto-merge-armed PR from a previous
release can never merge mid-release with unvalidated content. Fails closed —
nothing is published yet, so a demotion failure aborts the release run.

Only PRs whose head lives in the tap itself and whose author is devantler are
considered; a fork branch that happens to share the evergreen name is ignored.
EOF
}

tap=""
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
	usage
	exit 0
fi
if [[ "${1:-}" == --tap ]]; then
	tap="${2:-}"
	shift 2
fi
if [[ ! "${tap}" =~ ^[^/]+/[^/]+$ || $# -lt 1 ]]; then
	printf 'ERROR: --tap OWNER/REPO and at least one CASK_NAME are required\n' >&2
	usage >&2
	exit 2
fi
tap_owner="${tap%%/*}"

trusted_open_prs() {
	local branch="$1" matches
	if ! matches="$(gh api \
		"repos/${tap}/pulls?state=open&head=${tap_owner}:${branch}&per_page=100")"; then
		printf 'ERROR: could not enumerate open cask PRs on %s for branch %s\n' \
			"${tap}" "${branch}" >&2
		return 1
	fi
	if ! jq -ec --arg full "${tap}" '
      [.[] | select(.head.repo.full_name == $full and .user.login == "devantler")
        | {number, isDraft: .draft, id: .node_id}]
    ' <<<"${matches}"; then
		printf 'ERROR: could not filter cask PR candidates for branch %s\n' "${branch}" >&2
		return 1
	fi
}

current_branch_sha() {
	local branch="$1" refs exact count sha
	if ! refs="$(gh api "repos/${tap}/git/matching-refs/heads/${branch}")"; then
		printf 'ERROR: could not inspect retained cask branch %s on %s\n' "${branch}" "${tap}" >&2
		return 1
	fi
	if ! exact="$(jq -ec --arg ref "refs/heads/${branch}" \
		'[.[] | select(.ref == $ref)]' <<<"${refs}")"; then
		printf 'ERROR: malformed retained-branch response for %s\n' "${branch}" >&2
		return 1
	fi
	count="$(jq -r 'length' <<<"${exact}")"
	if [[ "${count}" -eq 0 ]]; then
		return 0
	fi
	if [[ "${count}" -ne 1 ]]; then
		printf 'ERROR: expected at most one retained branch ref for %s, found %s\n' \
			"${branch}" "${count}" >&2
		return 1
	fi
	sha="$(jq -r '.[0].object.sha // ""' <<<"${exact}")"
	if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
		printf 'ERROR: retained branch %s has an invalid object SHA\n' "${branch}" >&2
		return 1
	fi
	printf '%s\n' "${sha}"
}

prune_terminal_branch() {
	local branch="$1" branch_sha closed_pages evidence_count open_now current_sha remote_url
	if ! branch_sha="$(current_branch_sha "${branch}")"; then
		return 1
	fi
	if [[ -z "${branch_sha}" ]]; then
		printf 'No reusable open cask PR and no retained branch on %s for %s; GoReleaser will open a fresh draft\n' \
			"${tap}" "${branch}"
		return 0
	fi

	# A no-open-PR branch can still carry the history of a squash-merged evergreen PR. GoReleaser
	# appends to that retained history, producing a fresh PR that conflicts with main. Delete only
	# when a terminal PR record accounts for the branch's exact current SHA; otherwise preserve it.
	if ! closed_pages="$(gh api --paginate --slurp \
		"repos/${tap}/pulls?state=closed&head=${tap_owner}:${branch}&per_page=100")"; then
		printf 'ERROR: could not enumerate terminal cask PRs for retained branch %s\n' "${branch}" >&2
		return 1
	fi
	if ! jq -e 'type == "array" and all(.[]; type == "array")' \
		<<<"${closed_pages}" >/dev/null; then
		printf 'ERROR: malformed terminal-PR response for retained branch %s\n' "${branch}" >&2
		return 1
	fi
	evidence_count="$(jq -r \
		--arg full "${tap}" \
		--arg branch "${branch}" \
		--arg sha "${branch_sha}" '
      [
        .[][]
        | select(
            .state == "closed"
            and .user.login == "devantler"
            and .head.repo.full_name == $full
            and .head.ref == $branch
            and .head.sha == $sha
          )
      ]
      | length
    ' <<<"${closed_pages}")"
	if [[ "${evidence_count}" -eq 0 ]]; then
		printf 'ERROR: retained branch %s has no terminal PR evidence matching SHA %s; refusing to delete it\n' \
			"${branch}" "${branch_sha}" >&2
		return 1
	fi

	# Close the discovery-to-delete race as far as the APIs permit: recheck both the open-PR keep
	# set and the branch SHA immediately before a compare-and-swap delete. The force-with-lease
	# refuses if another actor moves the ref after this evidence was gathered.
	if ! open_now="$(trusted_open_prs "${branch}")"; then
		return 1
	fi
	if [[ "$(jq -r 'length' <<<"${open_now}")" -ne 0 ]]; then
		printf 'ERROR: retained branch %s acquired an open PR before deletion; refusing to delete it\n' \
			"${branch}" >&2
		return 1
	fi
	if ! current_sha="$(current_branch_sha "${branch}")"; then
		return 1
	fi
	if [[ "${current_sha}" != "${branch_sha}" ]]; then
		printf 'ERROR: retained branch %s moved before deletion; expected %s, found %s\n' \
			"${branch}" "${branch_sha}" "${current_sha:-missing}" >&2
		return 1
	fi
	if [[ -z "${GH_TOKEN:-}" ]]; then
		printf 'ERROR: GH_TOKEN is required for the exact-SHA retained-branch delete\n' >&2
		return 1
	fi
	remote_url="https://x-access-token:${GH_TOKEN}@github.com/${tap}.git"
	if ! git push --force-with-lease="refs/heads/${branch}:${branch_sha}" \
		"${remote_url}" ":refs/heads/${branch}" >/dev/null 2>&1; then
		printf 'ERROR: could not delete terminal branch %s at expected SHA %s; refusing to continue\n' \
			"${branch}" "${branch_sha}" >&2
		return 1
	fi
	printf 'Removed terminal branch %s at %s before GoReleaser creates the next PR\n' \
		"${branch}" "${branch_sha}"
}

for name in "$@"; do
	branch="goreleaser/${name}"
	# REST `head=<owner>:<branch>` filters SERVER-side (gh pr list --head is a bare
	# branch-name filter, so same-name fork branches could fill a client-side page
	# before any jq filter runs). The owner qualifier still admits a same-owner
	# sibling repo's branch, so keep the full_name + author checks below.
	if ! ours="$(trusted_open_prs "${branch}")"; then
		exit 1
	fi
	count="$(jq -r 'length' <<<"${ours}")"
	if [[ "${count}" -eq 0 ]]; then
		if ! prune_terminal_branch "${branch}"; then
			exit 1
		fi
		continue
	fi
	if [[ "${count}" -ne 1 ]]; then
		printf 'ERROR: expected at most one open tap-owned cask PR for %s, found %s\n' "${branch}" "${count}" >&2
		exit 1
	fi
	pr="$(jq -r '.[0].number' <<<"${ours}")"
	if [[ "$(jq -r '.[0].isDraft' <<<"${ours}")" == "true" ]]; then
		printf '%s#%s is already a draft; checkpoint holds\n' "${tap}" "${pr}"
		continue
	fi
	node_id="$(jq -r '.[0].id' <<<"${ours}")"
	# shellcheck disable=SC2016 # $id is a GraphQL variable, not a shell expansion.
	if ! gh api graphql \
		-f query='mutation($id:ID!){convertPullRequestToDraft(input:{pullRequestId:$id}){pullRequest{isDraft}}}' \
		-f id="${node_id}" >/dev/null; then
		printf 'ERROR: could not convert reused %s#%s back to draft; aborting before the branch is rewritten\n' \
			"${tap}" "${pr}" >&2
		exit 1
	fi
	printf 'Re-drafted reused %s#%s before rewriting %s\n' "${tap}" "${pr}" "${branch}"
done
