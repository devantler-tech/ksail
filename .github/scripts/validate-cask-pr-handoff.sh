#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  validate-cask-pr-handoff.sh --evidence FILE --tap OWNER/REPO
                              --cask-name NAME --tag TAG [--prepared]

Fail closed unless a GoReleaser cask PR is the expected trusted draft, from the
tap itself, into main, with exactly its matching cask file. With --prepared,
also require the normalized title and every requested label that exists in the
repository's available-label inventory.
EOF
}

evidence_file=""
tap=""
cask_name=""
tag=""
prepared=false

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
if ! jq -e . "${evidence_file}" >/dev/null 2>&1; then
	printf 'BLOCKED: evidence is not valid JSON\n' >&2
	exit 1
fi

expected_head="goreleaser/${cask_name}-${tag}"
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

if [[ "$(json_string '.pr.state | ascii_downcase')" != "open" ]]; then
	block 'PR state must be open'
fi
if ! jq -e '.pr.draft == true' "${evidence_file}" >/dev/null 2>&1; then
	block 'PR must remain a draft for maintainer promotion'
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
head_sha="$(json_string '.pr.head.sha')"
if [[ ! "${head_sha}" =~ ^[[:xdigit:]]{40}$ ]]; then
	block 'head SHA is missing or invalid'
fi

if ! jq -e '.files | type == "array"' "${evidence_file}" >/dev/null 2>&1; then
	block 'changed-file evidence is missing'
elif ! jq -e --arg path "${expected_path}" \
	'.files | length == 1 and .[0].filename == $path' \
	"${evidence_file}" >/dev/null 2>&1; then
	block "only ${expected_path} may change"
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

if [[ "${prepared}" == true ]]; then
	printf 'PASS: generated cask PR identity, scope, and metadata are valid at %s\n' "${head_sha}"
else
	printf 'PASS: generated cask PR identity and scope are valid at %s\n' "${head_sha}"
fi
