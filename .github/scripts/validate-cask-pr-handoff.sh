#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  validate-cask-pr-handoff.sh --evidence FILE --tap OWNER/REPO
                              --cask-name NAME --tag TAG [--prepared] [--ready] [--merged]

Fail closed unless a GoReleaser cask PR is the expected trusted draft, from the
tap itself, into main, with exactly its matching cask file. With --prepared,
also require the normalized title and every requested label that exists in the
repository's available-label inventory. With --ready, require the PR to already
be marked ready for review instead of a draft — used to revalidate an
already-handed-off PR on an idempotent rerun.
With --merged, require a trusted closed-and-merged PR whose exact cask blob is
still current on tap main. This mode intentionally does not trust the PR title:
an evergreen branch can receive the next release before its previous-titled PR
auto-merges, coalescing both releases into that older-titled PR.
EOF
}

evidence_file=""
tap=""
cask_name=""
tag=""
prepared=false
ready=false
merged=false

while (($# > 0)); do
	case "$1" in
	--evidence)
		evidence_file="${2:-}"
		shift 2
		;;
	--tap)
		tap="${2:-}"
		shift 2
		;;
	--cask-name)
		cask_name="${2:-}"
		shift 2
		;;
	--tag)
		tag="${2:-}"
		shift 2
		;;
	--prepared)
		prepared=true
		shift
		;;
	--ready)
		ready=true
		shift
		;;
	--merged)
		merged=true
		shift
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

if [[ -z "${evidence_file}" || -z "${tap}" || -z "${cask_name}" || -z "${tag}" ]]; then
	printf 'ERROR: --evidence, --tap, --cask-name, and --tag are required\n' >&2
	usage >&2
	exit 2
fi
if [[ ! -f "${evidence_file}" ]]; then
	printf 'ERROR: evidence file does not exist: %s\n' "${evidence_file}" >&2
	exit 2
fi
if [[ "${merged}" == true && ("${prepared}" == true || "${ready}" == true) ]]; then
	printf 'ERROR: --merged cannot be combined with --prepared or --ready\n' >&2
	exit 2
fi
if ! jq -e . "${evidence_file}" >/dev/null 2>&1; then
	printf 'BLOCKED: evidence is not valid JSON\n' >&2
	exit 1
fi

# Evergreen, version-less branch (mirrors .goreleaser*.yaml): each release updates the ONE
# open cask PR in place, so the head never carries the tag (tap#1172).
expected_head="goreleaser/${cask_name}"
expected_path="Casks/${cask_name}.rb"
expected_title="chore(cask): update ${cask_name} to ${tag}"
failures=()

block() {
	failures+=("$1")
}

json_string() {
	local expression="$1"
	jq -r "${expression} // empty" "${evidence_file}" 2>/dev/null || true
}

if [[ "${merged}" == true ]]; then
	if [[ "$(json_string '.pr.state | ascii_downcase')" != "closed" ]]; then
		block 'merged PR state must be closed'
	fi
	if ! jq -e '.pr.merged_at | type == "string" and length > 0' \
		"${evidence_file}" >/dev/null 2>&1; then
		block 'merged PR must have a merged_at timestamp'
	fi
	if ! jq -e '.pr.merged == true' "${evidence_file}" >/dev/null 2>&1; then
		block 'merged PR must be marked merged'
	fi
	if ! jq -e '.pr.draft == false' "${evidence_file}" >/dev/null 2>&1; then
		block 'merged PR must not be a draft'
	fi
else
	if [[ "$(json_string '.pr.state | ascii_downcase')" != "open" ]]; then
		block 'PR state must be open'
	fi
	if [[ "${ready}" == true ]]; then
		# Revalidating an already-handed-off PR: it has been promoted, so it must NOT be a draft.
		if ! jq -e '.pr.draft == false' "${evidence_file}" >/dev/null 2>&1; then
			block 'already-handed-off PR must be marked ready for review'
		fi
	else
		if ! jq -e '.pr.draft == true' "${evidence_file}" >/dev/null 2>&1; then
			block 'PR must remain a draft for maintainer promotion'
		fi
	fi
fi
if [[ "$(json_string '.pr.user.login')" != "devantler" ]]; then
	block 'PR author must be devantler'
fi
if [[ "$(json_string '.pr.head.repo.full_name')" != "${tap}" ]]; then
	block "head repository must exactly equal ${tap}"
fi
if [[ "$(json_string '.pr.head.ref')" != "${expected_head}" ]]; then
	block "head branch must exactly equal ${expected_head}"
fi
if [[ "$(json_string '.pr.base.ref')" != "main" ]]; then
	block 'base branch must exactly equal main'
fi
if [[ "${merged}" == true && "$(json_string '.pr.base.repo.full_name')" != "${tap}" ]]; then
	block "base repository must exactly equal ${tap}"
fi
head_sha="$(json_string '.pr.head.sha')"
if [[ ! "${head_sha}" =~ ^[[:xdigit:]]{40}$ ]]; then
	block 'head SHA is missing or invalid'
fi
merge_commit_sha=""
if [[ "${merged}" == true ]]; then
	merge_commit_sha="$(json_string '.pr.merge_commit_sha')"
	if [[ ! "${merge_commit_sha}" =~ ^[[:xdigit:]]{40}$ ]]; then
		block 'merge commit SHA is missing or invalid'
	fi
fi

if ! jq -e '.files | type == "array"' "${evidence_file}" >/dev/null 2>&1; then
	block 'changed-file evidence is missing'
elif ! jq -e --arg path "${expected_path}" \
	'.files | length == 1 and .[0].filename == $path' \
	"${evidence_file}" >/dev/null 2>&1; then
	block "only ${expected_path} may change"
fi
pr_file_sha=""
if [[ "${merged}" == true ]]; then
	pr_file_sha="$(json_string '.files[0].sha')"
	if [[ ! "${pr_file_sha}" =~ ^[[:xdigit:]]{40}$ ]]; then
		block 'merged PR cask blob SHA is missing or invalid'
	fi
fi

# Evergreen branches are reused across releases, so PR identity alone cannot prove
# the cask carries THIS release: require the head file content to pin the current
# version (a stale PR whose rewrite was skipped or failed must never be handed off).
expected_version="${tag#v}"
cask_content=""
if ! jq -e --arg path "${expected_path}" \
	'.headFile | type == "object" and .path == $path and (.content | type == "string")' \
	"${evidence_file}" >/dev/null 2>&1; then
	block 'cask head-content evidence is missing'
elif ! cask_content="$(jq -r '.headFile.content' "${evidence_file}" | base64 -d 2>/dev/null)"; then
	cask_content=""
	block 'cask head content is not valid base64'
elif [[ -z "${cask_content}" ]]; then
	block 'cask head content must not be empty'
fi
content_location='at head'
if [[ "${merged}" == true ]]; then
	head_file_sha="$(json_string '.headFile.sha')"
	if [[ -z "${pr_file_sha}" || "${head_file_sha}" != "${pr_file_sha}" ]]; then
		block 'merged PR head cask blob must match its changed-file blob'
	fi
	merge_file_sha=""
	if ! jq -e --arg path "${expected_path}" '
      .mergeFile
      | type == "object"
        and .path == $path
        and (.sha | type == "string")
        and (.content | type == "string")
    ' "${evidence_file}" >/dev/null 2>&1; then
		block 'merge-result cask evidence is missing'
	else
		merge_file_sha="$(json_string '.mergeFile.sha')"
		if [[ ! "${merge_file_sha}" =~ ^[[:xdigit:]]{40}$ || -z "${pr_file_sha}" ||
			"${merge_file_sha}" != "${pr_file_sha}" ]]; then
			block 'merge-result cask blob must match the merged PR changed-file blob'
		fi
	fi
	main_ref_sha="$(json_string '.mainRef.sha')"
	if [[ ! "${main_ref_sha}" =~ ^[[:xdigit:]]{40}$ ]]; then
		block 'pinned main SHA is missing or invalid'
	fi
	main_file_sha=""
	if ! jq -e --arg path "${expected_path}" '
      .mainFile
      | type == "object"
        and .path == $path
        and (.sha | type == "string")
        and (.content | type == "string")
    ' "${evidence_file}" >/dev/null 2>&1; then
		block 'current-main cask evidence is missing'
	else
		main_file_sha="$(json_string '.mainFile.sha')"
		if [[ ! "${main_file_sha}" =~ ^[[:xdigit:]]{40}$ || -z "${merge_file_sha}" ||
			"${main_file_sha}" != "${merge_file_sha}" ]]; then
			block 'merged PR cask blob must still be current on main'
		fi
		if ! cask_content="$(jq -r '.mainFile.content' "${evidence_file}" | base64 -d 2>/dev/null)"; then
			cask_content=""
			block 'current-main cask content is not valid base64'
		elif [[ -z "${cask_content}" ]]; then
			block 'current-main cask content must not be empty'
		fi
		content_location='on main'
	fi
	if ! jq -e '.mergeComparison | type == "object"' \
		"${evidence_file}" >/dev/null 2>&1; then
		block 'merge ancestry evidence is missing or invalid'
	else
		comparison_status="$(json_string '.mergeComparison.status')"
		comparison_behind="$(json_string '.mergeComparison.behind_by')"
		comparison_base="$(json_string '.mergeComparison.merge_base_commit.sha')"
		if [[ "${comparison_status}" != "ahead" && "${comparison_status}" != "identical" ]]; then
			block 'merge commit must be an ancestor of pinned main'
		fi
		if [[ "${comparison_behind}" != "0" ]]; then
			block 'merge commit must not be behind the pinned-main merge base'
		fi
		if [[ -z "${merge_commit_sha}" || "${comparison_base}" != "${merge_commit_sha}" ]]; then
			block 'pinned main must descend from the merge commit'
		fi
	fi
fi
if [[ -n "${cask_content}" ]] &&
	! grep -Fq "version \"${expected_version}\"" <<<"${cask_content}"; then
	block "cask ${content_location} must pin version ${expected_version}"
fi

# A version match alone is not enough on a rerun of the SAME tag: a stale evergreen PR
# from a failed earlier attempt already pins this version while its sha256 still points
# at the deleted draft release's artifacts. Require every sha256 the cask pins to equal
# a digest GitHub reports for the published release's assets.
if [[ -n "${cask_content}" ]]; then
	if ! jq -e '.releaseAssets | type == "array" and length > 0' \
		"${evidence_file}" >/dev/null 2>&1; then
		block 'release-asset digest evidence is missing'
	else
		cask_shas="$(grep -oE 'sha256 "[[:xdigit:]]{64}"' <<<"${cask_content}" |
			grep -oE '[[:xdigit:]]{64}' || true)"
		if [[ -z "${cask_shas}" ]]; then
			block 'cask at head must pin at least one sha256'
		else
			while IFS= read -r cask_sha; do
				if ! jq -e --arg digest "sha256:${cask_sha}" \
					'.releaseAssets | any(.digest == $digest)' \
					"${evidence_file}" >/dev/null 2>&1; then
					block "cask sha256 ${cask_sha} does not match any published release asset digest"
				fi
			done <<<"${cask_shas}"
		fi
	fi
fi

if [[ "${prepared}" == true ]]; then
	if [[ "$(json_string '.pr.title')" != "${expected_title}" ]]; then
		block "title must exactly equal ${expected_title}"
	fi
	if ! jq -e '.availableLabels | type == "array"' \
		"${evidence_file}" >/dev/null 2>&1; then
		block 'available-label inventory is missing'
	elif ! jq -e '
      .availableLabels
      | all(.[]; (.name | type == "string" and length > 0))
    ' "${evidence_file}" >/dev/null 2>&1; then
		block 'available-label inventory is malformed'
	else
		for label in automation dependencies; do
			if jq -e --arg label "${label}" \
				'.availableLabels | any(.name == $label)' \
				"${evidence_file}" >/dev/null 2>&1 &&
				! jq -e --arg label "${label}" \
					'.pr.labels | type == "array" and any(.name == $label)' \
					"${evidence_file}" >/dev/null 2>&1; then
				block "available label is missing from PR: ${label}"
			fi
		done
	fi
fi

if ((${#failures[@]} > 0)); then
	printf 'BLOCKED: %d cask PR handoff validation(s) failed\n' "${#failures[@]}" >&2
	for failure in "${failures[@]}"; do
		printf -- '- %s\n' "${failure}" >&2
	done
	exit 1
fi

if [[ "${merged}" == true ]]; then
	printf 'PASS: merged generated cask PR identity and scope are valid at %s\n' "${head_sha}"
elif [[ "${prepared}" == true ]]; then
	printf 'PASS: generated cask PR identity, scope, and metadata are valid at %s\n' "${head_sha}"
else
	printf 'PASS: generated cask PR identity and scope are valid at %s\n' "${head_sha}"
fi
