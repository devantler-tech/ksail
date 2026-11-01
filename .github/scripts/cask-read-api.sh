#!/usr/bin/env bash
# Read cask evidence, recovering once from primary quota exhaustion inside the
# caller's existing shared deadline. Never replay a mutation or partial response.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
export GH_TOKEN="${KSAIL_CASK_READ_TOKEN:-${GH_TOKEN:-}}"
[[ $# -gt 0 ]] || exit 2
query_found=false
for arg in "$@"; do
	case "$arg" in
	-X* | --method* | --input* | --hostname*)
		printf 'ERROR: cask evidence accepts read operations only\n' >&2
		exit 2
		;;
	-f* | -F* | --field* | --raw-field*)
		[[ "$1" == graphql ]] || exit 2
		;;
	query=*)
		query_document="${arg#query=}"
		# The internal callers use explicit read documents. Reject a second
		# mutation operation as well as a mutation at the document's beginning.
		[[ "$1" == graphql && "$query_document" =~ ^[[:space:]]*query([[:space:]]|\{) &&
			! "$query_document" =~ (^|[^[:alnum:]_])mutation([^[:alnum:]_]|$) ]] || exit 2
		query_found=true
		;;
	esac
done
[[ "$1" != graphql || "$query_found" == true ]] || exit 2

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
status=0
gh api "$@" >"$work/output" 2>"$work/error" || status=$?
if [[ "$status" -ne 0 ]] &&
	LC_ALL=C grep -Eq '^gh: API rate limit exceeded([[:space:]]|$)' "$work/error"; then
	# No new wait window: interactive callers without the production deadline
	# retain their immediate refusal. This also excludes secondary-limit retries.
	if [[ ! "${KSAIL_GITHUB_WRITE_BUDGET_DEADLINE:-}" =~ ^[1-9][0-9]{0,9}$ ]]; then
		printf 'BLOCKED: primary read quota exhausted without a bounded recovery window\n' >&2
		exit "$status"
	fi
	printf 'Primary read quota exhausted; checking recovery within the existing deadline.\n' >&2
	"${script_dir}/wait-for-github-api-budget.sh" \
		--min-core 1 --min-graphql 1 --require-readable >&2 || exit 1
	# The finisher owns this private generation counter. A recovered read can
	# invalidate gates collected before the wait, even when quota is now high.
	if [[ -n "${KSAIL_CASK_READ_RECOVERY_MARKER:-}" ]]; then
		printf '1\n' >>"$KSAIL_CASK_READ_RECOVERY_MARKER" || exit 1
	fi
	status=0
	gh api "$@" >"$work/output" 2>"$work/error" || status=$?
fi
if [[ "$status" -ne 0 ]]; then
	printf 'BLOCKED: GitHub evidence read failed (exit %s); no partial response accepted\n' "$status" >&2
	exit "$status"
fi
cat "$work/output"
