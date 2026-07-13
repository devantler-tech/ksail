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
tap_repo="${tap#*/}"

for name in "$@"; do
	branch="goreleaser/${name}"
	if ! matches="$(gh pr list --repo "${tap}" --head "${branch}" --state open \
		--limit 10 --json number,isDraft,id,author,headRepositoryOwner,headRepository)"; then
		printf 'ERROR: could not enumerate open cask PRs on %s for branch %s\n' "${tap}" "${branch}" >&2
		exit 1
	fi
	# `--head` filters by branch NAME only, so a branch named goreleaser/<name> in a fork
	# OR in another same-owner repository would match too — keep only the PR whose head
	# lives in the tap itself (owner AND repo name) and is devantler-authored.
	if ! ours="$(jq -ec --arg owner "${tap_owner}" --arg repo "${tap_repo}" '
      [.[] | select(.headRepositoryOwner.login == $owner
        and .headRepository.name == $repo
        and .author.login == "devantler")]
    ' <<<"${matches}")"; then
		printf 'ERROR: could not filter cask PR candidates for branch %s\n' "${branch}" >&2
		exit 1
	fi
	count="$(jq -r 'length' <<<"${ours}")"
	if [[ "${count}" -eq 0 ]]; then
		printf 'No reusable open cask PR on %s for %s; GoReleaser will open a fresh draft\n' "${tap}" "${branch}"
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
