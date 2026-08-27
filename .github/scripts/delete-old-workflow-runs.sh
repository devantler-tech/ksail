#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  delete-old-workflow-runs.sh --repository OWNER/REPO --retain-days DAYS
                              --keep-minimum-runs COUNT
                              --max-deletions COUNT

Delete a bounded batch of completed runs from active repository workflows.
Runs newer than the retention cutoff, the newest COUNT runs per workflow,
runs linked to pull requests, and runs on existing non-default branches are
preserved. Orphan runs from deleted or organization-required workflows are
deliberately outside this cleanup boundary.

GH_TOKEN must contain actions:write for OWNER/REPO. COUNT for
--max-deletions is capped at 750 and counts every deletion attempt so one
execution stays within the repository-scoped GITHUB_TOKEN API budget.
EOF
}

list_contains() {
	local needle="$1"
	local list="$2"
	local item

	while IFS= read -r item; do
		if [[ "${item}" == "${needle}" ]]; then
			return 0
		fi
	done <<<"${list}"

	return 1
}

repository=""
retain_days=""
keep_minimum_runs=""
max_deletions=""

while (($# > 0)); do
	case "$1" in
	--repository)
		repository="${2:-}"
		shift 2
		;;
	--retain-days)
		retain_days="${2:-}"
		shift 2
		;;
	--keep-minimum-runs)
		keep_minimum_runs="${2:-}"
		shift 2
		;;
	--max-deletions)
		max_deletions="${2:-}"
		shift 2
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		printf 'ERROR: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "${repository}" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ||
	! "${retain_days}" =~ ^[1-9][0-9]*$ ||
	! "${keep_minimum_runs}" =~ ^[1-9][0-9]*$ ||
	! "${max_deletions}" =~ ^[1-9][0-9]*$ ||
	"${keep_minimum_runs}" -gt 100 || "${max_deletions}" -gt 750 ]]; then
	printf 'ERROR: valid repository, positive counts, keep <= 100, and max deletions <= 750 are required\n' >&2
	usage >&2
	exit 2
fi

if [[ -z "${GH_TOKEN:-}" ]]; then
	printf 'ERROR: GH_TOKEN is required\n' >&2
	exit 2
fi

cutoff_date="${KSAIL_MAINTENANCE_CUTOFF_DATE:-}"
if [[ -z "${cutoff_date}" ]]; then
	if cutoff_date="$(date -u -d "${retain_days} days ago" +%Y-%m-%d 2>/dev/null)"; then
		:
	else
		cutoff_date="$(date -u -v-"${retain_days}"d +%Y-%m-%d)"
	fi
fi
if [[ ! "${cutoff_date}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
	printf 'ERROR: invalid retention cutoff: %s\n' "${cutoff_date}" >&2
	exit 2
fi

default_branch="$(gh api "repos/${repository}" --jq '.default_branch')"
if [[ -z "${default_branch}" ]]; then
	printf 'ERROR: repository default branch is empty\n' >&2
	exit 1
fi

branch_output="$(
	gh api --paginate "repos/${repository}/branches?per_page=100" --jq '.[] | .name'
)"

workflow_output="$(
	gh api --paginate "repos/${repository}/actions/workflows?per_page=100" \
		--jq '.workflows[] | select(.state == "active") | .id'
)"
workflow_ids=()
while IFS= read -r workflow_id; do
	if [[ "${workflow_id}" =~ ^[1-9][0-9]*$ ]]; then
		workflow_ids+=("${workflow_id}")
	elif [[ -n "${workflow_id}" ]]; then
		printf 'ERROR: invalid workflow ID: %s\n' "${workflow_id}" >&2
		exit 1
	fi
done <<<"${workflow_output}"

deletion_attempts=0
deletion_failures=0
deletions=0
already_absent=0
for workflow_id in "${workflow_ids[@]}"; do
	keep_output="$(
		gh api --method GET "repos/${repository}/actions/workflows/${workflow_id}/runs" \
			-f "per_page=${keep_minimum_runs}" --jq '.workflow_runs[].id'
	)"
	while IFS= read -r keep_id; do
		if [[ "${keep_id}" =~ ^[1-9][0-9]*$ ]]; then
			:
		elif [[ -n "${keep_id}" ]]; then
			printf 'ERROR: invalid workflow run ID: %s\n' "${keep_id}" >&2
			exit 1
		fi
	done <<<"${keep_output}"

	candidate_output="$(
		gh api --method GET "repos/${repository}/actions/workflows/${workflow_id}/runs" \
			-f per_page=100 -f "created=<${cutoff_date}" \
			--jq '.workflow_runs[] | select(.status == "completed") | {id, head_branch: (.head_branch // ""), pull_request_count: (.pull_requests | length)}'
	)"
	while IFS= read -r candidate; do
		if [[ -z "${candidate}" ]]; then
			continue
		fi

		run_id="$(jq -er '.id | select(type == "number")' <<<"${candidate}")"
		head_branch="$(jq -er '.head_branch | select(type == "string")' <<<"${candidate}")"
		pull_request_count="$(
			jq -er '.pull_request_count | select(type == "number")' <<<"${candidate}"
		)"

		if list_contains "${run_id}" "${keep_output}" || [[ "${pull_request_count}" -gt 0 ]]; then
			continue
		fi
		if [[ -n "${head_branch}" && "${head_branch}" != "${default_branch}" ]] &&
			list_contains "${head_branch}" "${branch_output}"; then
			continue
		fi

		((deletion_attempts += 1))
		if delete_output="$(
			gh api --method DELETE "repos/${repository}/actions/runs/${run_id}" 2>&1
		)"; then
			((deletions += 1))
			printf 'Deleted workflow run %s (%s)\n' "${run_id}" "${workflow_id}"
		elif [[ "${delete_output}" == *"(HTTP 404)"* ]]; then
			# The run vanished between the listing above and this delete — a
			# concurrent cleanup pass or GitHub's own retention got there first.
			# The run is gone, which is the outcome this script wanted, so it is
			# not a failed attempt. Reported so the batch stays auditable.
			((already_absent += 1))
			printf 'Already absent: workflow run %s (%s)\n' "${run_id}" "${workflow_id}"
		else
			((deletion_failures += 1))
			printf 'ERROR: failed to delete workflow run %s (%s): %s\n' \
				"${run_id}" "${workflow_id}" "${delete_output}" >&2
		fi

		if ((deletion_attempts == max_deletions)); then
			printf 'Deletion limit reached: %s\n' "${max_deletions}"
			break 2
		fi
	done <<<"${candidate_output}"
done

if ((deletion_failures > 0)); then
	printf 'ERROR: Cleanup completed with %s failed deletion attempt(s)\n' \
		"${deletion_failures}" >&2
	exit 1
fi

printf 'Cleanup complete: %s workflow run(s) deleted, %s already absent\n' \
	"${deletions}" "${already_absent}"
