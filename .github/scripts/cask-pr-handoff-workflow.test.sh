#!/usr/bin/env bash
# shellcheck disable=SC2016 # Assertions intentionally match literal workflow shell variables.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
cd_workflow="${repo_root}/.github/workflows/cd.yaml"
ci_workflow="${repo_root}/.github/workflows/ci.yaml"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
homebrew_block="${tmp_dir}/homebrew.yaml"
execution_surface="${tmp_dir}/execution-surface.sh"

awk '
  /^  homebrew:/ { in_homebrew = 1 }
  /^  cleanup-failed-release:/ { in_homebrew = 0 }
  in_homebrew { print }
' "${cd_workflow}" >"${homebrew_block}"

awk '1' \
	"${homebrew_block}" \
	"${repo_root}/.github/scripts/collect-cask-pr-handoff.sh" \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	>"${execution_surface}"

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	exit 1
}

assert_contains() {
	local pattern="$1" file="$2" message="$3"
	grep -Fq -- "${pattern}" "${file}" || fail "${message}"
}

assert_not_contains() {
	local pattern="$1" file="$2" message="$3"
	if grep -Fq -- "${pattern}" "${file}"; then
		fail "${message}"
	fi
}

assert_contains 'name: 🍺 Prepare Homebrew cask PRs' "${homebrew_block}" \
	'release job must be a prepare-and-handoff step'
assert_contains 'validate-cask-pr-handoff.sh' "${homebrew_block}" \
	'release job must validate generated PR identity and file scope'
assert_contains 'collect-cask-pr-handoff.sh' "${homebrew_block}" \
	'release job must use the behavior-tested paginated collector'
assert_contains 'title="chore(cask): update ${name} to ${TAG}"' "${homebrew_block}" \
	'release job must normalize the Conventional title'
assert_contains 'for label in automation dependencies' "${homebrew_block}" \
	'release job must add available automation/dependencies labels'
assert_contains 'remains draft/open for maintainer promotion' "${homebrew_block}" \
	'release job must record the draft PR handoff'
assert_contains 'convertPullRequestToDraft' "${homebrew_block}" \
	'release job must re-arm the draft checkpoint on a reused promoted evergreen PR'
assert_not_contains 'gh pr ready' "${execution_surface}" \
	'release job and its helpers must never self-promote a generated draft'
assert_not_contains 'gh pr merge' "${execution_surface}" \
	'release job and its helpers must never merge a generated cask PR'
assert_not_contains '--auto' "${execution_surface}" \
	'release job and its helpers must never arm auto-merge'
assert_not_contains '--admin' "${execution_surface}" \
	'release job and its helpers must never bypass branch protections'

validator_calls="$(grep -Fc -- 'validate_handoff "$pr"' "${homebrew_block}" || true)"
if [ "${validator_calls}" -lt 3 ]; then
	fail "expected pre-style, post-style, and prepared validation; found ${validator_calls} calls"
fi

assert_contains 'if: always() && needs.publish-release.result != '\''success'\''' "${cd_workflow}" \
	'a pending cask handoff must not delete a published release'
assert_contains 'cask-pr-handoff:' "${ci_workflow}" \
	'CI must include the cask PR handoff fixture job'
assert_contains '.github/scripts/validate-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must execute the handoff fixture suite'
assert_contains '.github/scripts/collect-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must execute the handoff collector suite'
assert_contains '.github/scripts/cask-pr-handoff-workflow.test.sh' "${ci_workflow}" \
	'CI must execute the handoff workflow contract suite'

printf 'Cask PR handoff workflow contract passed.\n'
