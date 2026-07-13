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
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
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
assert_contains 'pulls?state=open&head=' "${homebrew_block}" \
	'release job must enumerate cask PRs with the server-side head filter (no client-side page cap)'
assert_contains 'head.repo.full_name == $full' "${homebrew_block}" \
	'release job must scope matches to the tap-owned devantler PR (full head-repo name)'
assert_contains '--source-repo "$GITHUB_REPOSITORY"' "${homebrew_block}" \
	'release job must collect release-asset digest evidence for the sha256 handoff check'
# Cask PRs are a trusted programmed release path (maintainer direction ksail#6095): after full
# validation the job marks the PR ready so the tap's checks gate its auto-merge — but ONLY after
# the prepared validation, and never by merging or bypassing anything itself.
assert_contains 'gh pr ready "$pr" --repo "$TAP"' "${homebrew_block}" \
	'release job must hand the validated cask PR to the tap check-gated auto-merge path'
assert_contains 'marked ready for check-gated auto-merge' "${homebrew_block}" \
	'release job must record the ready-for-auto-merge handoff'
assert_not_contains 'gh pr merge' "${execution_surface}" \
	'release job and its helpers must never merge a generated cask PR'
assert_not_contains '--auto' "${execution_surface}" \
	'release job and its helpers must never arm auto-merge themselves'
assert_not_contains '--admin' "${execution_surface}" \
	'release job and its helpers must never bypass branch protections'

validator_calls="$(grep -Fc -- 'validate_handoff "$pr"' "${homebrew_block}" || true)"
if [ "${validator_calls}" -lt 3 ]; then
	fail "expected pre-style, post-style, and prepared validation; found ${validator_calls} calls"
fi
ready_line="$(grep -Fn -- 'gh pr ready "$pr" --repo "$TAP"' "${homebrew_block}" | head -1 | cut -d: -f1)"
prepared_line="$(grep -Fn -- '"$evidence" prepared' "${homebrew_block}" | head -1 | cut -d: -f1)"
if [ -z "${ready_line}" ] || [ -z "${prepared_line}" ] || [ "${ready_line}" -le "${prepared_line}" ]; then
	fail 'the ready-for-auto-merge handoff must come after the prepared validation'
fi

# The pre-release checkpoint: a reused, still-promoted evergreen PR is demoted BEFORE GoReleaser
# rewrites its branch, so the tap can never auto-merge unvalidated freshly-rewritten content.
assert_contains 'cask-checkpoint:' "${cd_workflow}" \
	'cd must define the pre-release cask draft checkpoint job'
assert_contains 'redraft-evergreen-cask-prs.sh --tap devantler-tech/homebrew-tap ksail ksail-desktop' "${cd_workflow}" \
	'the checkpoint job must re-draft both evergreen cask PRs'
if [ "$(grep -Fc -- 'needs: [cask-checkpoint]' "${cd_workflow}")" -lt 2 ]; then
	fail 'both cask-writing release jobs (goreleaser, desktop-macos) must gate on cask-checkpoint'
fi
assert_contains 'convertPullRequestToDraft' \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	'the checkpoint script must demote via the draft conversion mutation'
assert_contains 'pulls?state=open&head=' \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	'the checkpoint script must enumerate cask PRs with the server-side head filter (no client-side page cap)'
assert_contains 'head.repo.full_name == $full' \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	'the checkpoint script must scope matches to the tap-owned devantler PR (full head-repo name)'
# The checkpoint script is release-critical: CI must both re-run this suite when it
# changes and shellcheck it (a filter/shellcheck omission lets it break unnoticed).
assert_contains "- '.github/scripts/redraft-evergreen-cask-prs.sh'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on checkpoint-script changes'
if [ "$(grep -Fc -- '.github/scripts/redraft-evergreen-cask-prs.sh' "${ci_workflow}")" -lt 2 ]; then
	fail 'CI must also shellcheck the checkpoint script, not just path-filter it'
fi
assert_contains 'release-asset digest evidence is missing' \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	'the validator must require release-asset digest evidence'
assert_contains 'does not match any published release asset digest' \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	'the validator must verify cask sha256 stanzas against published asset digests'

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
