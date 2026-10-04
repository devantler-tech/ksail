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
selected_open_block="${tmp_dir}/selected-open-block.sh"
reconcile_function="${tmp_dir}/reconcile-merged-handoff.sh"

awk '
  /^  homebrew:/ { in_homebrew = 1 }
  /^  cleanup-failed-release:/ { in_homebrew = 0 }
  in_homebrew { print }
' "${cd_workflow}" >"${homebrew_block}"

awk '1' \
	"${homebrew_block}" \
	"${repo_root}/.github/scripts/collect-cask-pr-handoff.sh" \
	"${repo_root}/.github/scripts/find-merged-cask-pr-handoff.sh" \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	"${repo_root}/.github/scripts/finish-cask-pr-handoff.sh" \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	>"${execution_surface}"

# Once an open PR is selected it can auto-merge between any API call. No later failure may set the
# global failure bit directly: it must first pass through the immutable merged-current-main proof.
awk '
  /pr="\$\(jq -r '\''\.\[0\]\.number'\''/ { in_selected_open = 1 }
  /if \[ "\$handoff_failed" -ne 0 \]/ { in_selected_open = 0 }
  in_selected_open { print }
' "${homebrew_block}" >"${selected_open_block}"

# Exercise the workflow's real reconciliation function, not a copied test helper. A selected open
# PR can merge after the finder's first closed-PR enumeration, so the function must retry within a
# fixed bound before it records a failed handoff.
awk '
  /^[[:space:]]+reconcile_merged_handoff\(\) \{/ { in_function = 1 }
  in_function {
    line = $0
    sub(/^          /, "", line)
    print line
  }
  in_function && /^          }[[:space:]]*$/ { exit }
' "${homebrew_block}" >"${reconcile_function}"

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

race_root="${tmp_dir}/merged-race"
mkdir -p "${race_root}/.github/scripts" "${race_root}/evidence"
cat >"${race_root}/.github/scripts/find-merged-cask-pr-handoff.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
calls=0
if [[ -f "${RECONCILE_RACE_STATE}" ]]; then
	calls="$(<"${RECONCILE_RACE_STATE}")"
fi
calls=$((calls + 1))
printf '%s\n' "${calls}" >"${RECONCILE_RACE_STATE}"
if [[ "${calls}" -eq 1 ]]; then
	printf 'simulated just-merged list lag\n' >&2
	exit 1
fi
printf '42\n'
EOF
chmod +x "${race_root}/.github/scripts/find-merged-cask-pr-handoff.sh"
(
	cd "${race_root}"
	TAP="devantler-tech/homebrew-tap"
	GITHUB_REPOSITORY="devantler-tech/ksail"
	TAG="v7.175.1"
	evidence_dir="${race_root}/evidence"
	GITHUB_STEP_SUMMARY="${race_root}/summary.md"
	RECONCILE_RACE_STATE="${race_root}/calls"
	export TAP GITHUB_REPOSITORY TAG evidence_dir GITHUB_STEP_SUMMARY RECONCILE_RACE_STATE
	handoff_failed=0
	# shellcheck disable=SC2317,SC2329 # Invoked by the sourced workflow function.
	sleep() { :; }
	# shellcheck source=/dev/null
	source "${reconcile_function}"
	reconcile_merged_handoff ksail 'simulated open-to-merged race'
	[[ "${handoff_failed}" -eq 0 ]] || exit 1
	[[ "$(<"${RECONCILE_RACE_STATE}")" -eq 2 ]] || exit 1
) || fail 'merged reconciliation must retry a just-merged PR before recording failure'

# Run the workflow's actual ready-retry block. A completed validation and a failed
# merge are different outcomes: the operator must receive the delivery failure.
ready_retry_block="${tmp_dir}/ready-retry.sh"
awk '
  /if \[ "\$pr_is_draft" = "false" \] &&/ { in_retry = 1 }
  in_retry && /# The cask-checkpoint job re-drafted/ { exit }
  in_retry { line = $0; sub(/^          /, "", line); print line }
' "${homebrew_block}" >"${ready_retry_block}"
retry_root="${tmp_dir}/ready-retry"
mkdir -p "${retry_root}/.github/scripts"
cat >"${retry_root}/.github/scripts/finish-cask-pr-handoff.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'called\n' >>"${READY_RETRY_CALLS}"
if [[ "${READY_RETRY_SCENARIO}" == delivery-failed ]]; then
	printf 'BLOCKED: asynchronous result read unavailable\n' >&2
	exit 1
fi
printf 'PASS: verified exact-head merge completed\n'
EOF
chmod +x "${retry_root}/.github/scripts/finish-cask-pr-handoff.sh"
for retry_case in validation-failed delivery-failed delivered; do
	retry_log="${tmp_dir}/${retry_case}.log"
	retry_summary="${tmp_dir}/${retry_case}.summary"
	(
		cd "${retry_root}"
		READY_RETRY_SCENARIO="${retry_case}"
		READY_RETRY_CALLS="${tmp_dir}/${retry_case}.calls"
		export READY_RETRY_SCENARIO READY_RETRY_CALLS
		TAP="devantler-tech/homebrew-tap" TAG="v7.175.1" pr=42
		GITHUB_REPOSITORY="devantler-tech/ksail" GITHUB_STEP_SUMMARY="${retry_summary}"
		pr_is_draft=false
		evidence="${tmp_dir}/unused-evidence.json" handoff_failed=0
		export TAP TAG pr GITHUB_REPOSITORY GITHUB_STEP_SUMMARY pr_is_draft evidence
		: >"${READY_RETRY_CALLS}"
		: >"${GITHUB_STEP_SUMMARY}"
		# shellcheck disable=SC2317,SC2329 # Invoked by the actual sourced workflow block.
		validate_handoff() {
			if [[ "${READY_RETRY_SCENARIO}" == validation-failed ]]; then
				printf 'scope mismatch\n' >&2
				return 1
			fi
			printf 'validation fixture passed\n'
		}
		# shellcheck disable=SC2317,SC2329 # Invoked by the actual sourced workflow block.
		reconcile_merged_handoff() {
			printf 'reconciled: %s\n' "$2"
			handoff_failed=1
		}
		for name in ksail ksail-desktop; do
			pr_title="chore(cask): update ${name} to ${TAG}"
			export pr_title
			# shellcheck source=/dev/null
			source "${ready_retry_block}"
		done
		if [[ "${READY_RETRY_SCENARIO}" == delivered ]]; then
			[[ "${handoff_failed}" -eq 0 ]] || exit 1
		else
			[[ "${handoff_failed}" -eq 1 ]] || exit 1
		fi
		if [[ "${READY_RETRY_SCENARIO}" == validation-failed ]]; then
			[[ ! -s "${READY_RETRY_CALLS}" ]] || exit 1
		else
			[[ "$(wc -l <"${READY_RETRY_CALLS}" | tr -d ' ')" -eq 2 ]] || exit 1
		fi
	) >"${retry_log}" 2>&1 || fail "ready retry ${retry_case} must retain its actual outcome"
	case "${retry_case}" in
	validation-failed)
		assert_contains 'failed prepared revalidation: scope mismatch' "${retry_log}" \
			'validation failure must retain its scope diagnostic'
		assert_contains 'scope mismatch' "${retry_summary}" \
			'validation failure must reach the step summary'
		;;
	delivery-failed)
		assert_contains 'asynchronous result read unavailable' "${retry_log}" \
			'delivery failure must retain the finisher diagnostic'
		assert_contains 'asynchronous result read unavailable' "${retry_summary}" \
			'delivery failure must reach the step summary'
		assert_not_contains 'revalidation failed' "${retry_summary}" \
			'a successful validation must not be reported as failed'
		assert_not_contains 'validation fixture passed' "${retry_summary}" \
			'successful validation output must not become the delivery failure reason'
		;;
	delivered)
		assert_contains 'delivered for v7.175.1' "${retry_log}" \
			'successful ready retry must still report completed delivery'
		assert_contains 'delivered for v7.175.1' "${retry_summary}" \
			'successful ready retry must reach the step summary'
		assert_not_contains 'reconciled:' "${retry_log}" \
			'a completed delivery must not enter failure reconciliation'
		;;
	esac
done

assert_contains 'name: 🍺 Deliver Homebrew cask PRs' "${homebrew_block}" \
	'release job must complete cask delivery'
# Both casks can consume the full pre-promotion, online-audit, and async-merge wait
# bounds. Leave ten minutes for checkout, evidence collection, and cold brew style.
finisher="${repo_root}/.github/scripts/finish-cask-pr-handoff.sh"
poll_attempts="$(sed -nE 's/^attempts=([0-9]+) interval=[0-9]+$/\1/p' "${finisher}")"
poll_interval="$(sed -nE 's/^attempts=[0-9]+ interval=([0-9]+)$/\1/p' "${finisher}")"
job_minutes="$(awk '/timeout-minutes:/ { print $2 }' "${homebrew_block}")"
for budget in "${poll_attempts}" "${poll_interval}" "${job_minutes}"; do
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] || fail 'cask delivery wait budget is not completely observed'
done
required_seconds=$((2 * 3 * poll_attempts * poll_interval + 10 * 60))
if ((job_minutes * 60 < required_seconds)); then
	fail 'release job timeout cannot accommodate both casks full independent wait bounds and setup'
fi
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
# A partial two-cask release leaves the job red with the first cask already promoted; a rerun must be
# idempotent — an already-ready, correctly-titled cask PR for this tag is treated as done rather than
# re-validated against the DRAFT requirement (which would fail the rerun forever). But "ready + right
# title" is not proof the tap will merge the correct cask, so the rerun re-runs the non-draft
# (`prepared`) validator first; only a revalidated ready PR is skipped. See #6134.
assert_contains 'delivered for ${TAG} (ready retry, revalidated and merged)' "${homebrew_block}" \
	'release job must complete an already-ready retry through validated merging'
assert_contains 'failed prepared revalidation' "${homebrew_block}" \
	'release job must fail the rerun red when an already-ready cask PR no longer passes prepared revalidation'
# A partial rerun may find the first cask ALREADY MERGED by the tap auto-merge; the open-only query
# then returns zero, so the job must check for a merged, this-tag cask before failing red (#6134).
assert_contains 'find-merged-cask-pr-handoff.sh' "${homebrew_block}" \
	'release job must route every zero-open-PR result through the fully validated merged finder'
assert_contains 'reconcile_merged_handoff "$name"' "${selected_open_block}" \
	'open-PR failures must retry through the merged-current-main proof after an auto-merge race'
assert_not_contains 'handoff_failed=1' "${selected_open_block}" \
	'no failure after selecting an open PR may bypass merged-current-main reconciliation'
reconciliation_calls="$(grep -Fc -- 'reconcile_merged_handoff "$name"' \
	"${selected_open_block}" || true)"
if [[ "${reconciliation_calls}" -ne 10 ]]; then
	fail "every selected-open failure must reconcile an auto-merge race; expected 10 call sites, found ${reconciliation_calls}"
fi
for context in \
	'failed ready-PR revalidation' \
	'failed draft validation after open enumeration' \
	'brew became unavailable while preparing' \
	'could not be confirmed brew-style-clean' \
	'failed post-style validation' \
	'could not receive normalized metadata' \
	'metadata could not be completed' \
	'failed prepared validation' \
	'could not be delivered'; do
	assert_contains "${context}" "${selected_open_block}" \
		"selected-open failure is missing merged reconciliation context: ${context}"
done
assert_not_contains 'and .title == $title' "${homebrew_block}" \
	'release job must not trust an exact merged PR title without immutable cask evidence'
assert_contains 'pulls?state=closed&head=' \
	"${repo_root}/.github/scripts/find-merged-cask-pr-handoff.sh" \
	'release job must check for an already-merged cask PR on a rerun (open-only query misses it)'
assert_contains '--include-main' \
	"${repo_root}/.github/scripts/find-merged-cask-pr-handoff.sh" \
	'merged finder must collect pinned-main and immutable merge evidence before accepting a handoff'
assert_contains '--merged' \
	"${repo_root}/.github/scripts/find-merged-cask-pr-handoff.sh" \
	'merged finder must apply the fail-closed merged validator to every candidate'
# A generated cask the runner could not style-clean (clone/tempdir/push failure) would be rejected by
# the tap's brew-style gate, silently blocking auto-merge — the job must go red, not promote it (#6134).
assert_contains 'could not ensure it is brew-style-clean' "${homebrew_block}" \
	'release job must fail red (not promote) when it cannot ensure the cask is brew-style-clean'
# The tap's CI autocorrects the same branch, so a bare push loses that race and strands a clean cask
# (#6628). The job must style-clean through the script that re-checks a moved tip.
assert_contains '.github/scripts/style-clean-cask-branch.sh' "${homebrew_block}" \
	'release job must style-clean casks through the race-tolerant script (#6628)'
assert_not_contains 'style: brew style --fix generated cask' "${homebrew_block}" \
	'release job must not commit and push autocorrections itself; a bare push fails when the tap CI autocorrects first (#6628)'
assert_contains '.github/scripts/style-clean-cask-branch.test.sh' "${ci_workflow}" \
	'CI must execute the cask style-clean race suite'
assert_contains '--source-repo "$GITHUB_REPOSITORY"' "${homebrew_block}" \
	'release job must collect release-asset digest evidence for the sha256 handoff check'
# Cask PRs are a trusted programmed release path (maintainer direction ksail#6095): after full
# validation the job promotes, verifies the actual online audit and completes an exact-head merge.
# Server-side rules must remain enforced even for a token with administrative bypass capability.
# The handoff drives `markPullRequestReadyForReview` directly rather than `gh pr ready`: that
# subcommand resolves the viewer's `login`, a scope the tap token does not have, so it failed on every
# release and stranded each cask PR as a draft (#6134). Assert the mechanism that actually promotes.
assert_contains 'markPullRequestReadyForReview' "${execution_surface}" \
	'release job must promote through the guarded completion helper'
assert_not_contains 'gh pr ready' "${execution_surface}" \
	'release job must not promote via `gh pr ready` (needs a login scope the tap token lacks; see #6134)'
assert_contains '.github/scripts/finish-cask-pr-handoff.sh' "${homebrew_block}" \
	'release job must complete a validated cask through the guarded exact-head merge helper'
assert_contains '.github/scripts/finish-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must execute the guarded cask completion behavior tests'
assert_not_contains '--auto' "${execution_surface}" \
	'release job and its helpers must never arm auto-merge themselves'
assert_not_contains '--admin' "${execution_surface}" \
	'release job and its helpers must never bypass branch protections'
assert_contains '/merge-async' "${execution_surface}" \
	'cask delivery must use the merge endpoint with explicit rule enforcement'
assert_contains '-F bypass_rules=false' "${execution_surface}" \
	'cask delivery must explicitly refuse administrative rule bypass'
assert_contains 'merge_action=direct_merge' "${execution_surface}" \
	'cask delivery must request a direct merge rather than an enduring queue decision'
assert_contains 'X-GitHub-Api-Version: 2026-03-10' "${execution_surface}" \
	'cask delivery must select the documented async merge API version'

validator_calls="$(grep -Fc -- 'validate_handoff "$pr"' "${homebrew_block}" || true)"
if [ "${validator_calls}" -lt 3 ]; then
	fail "expected pre-style, post-style, and prepared validation; found ${validator_calls} calls"
fi
ready_line="$(grep -Fn -- '.github/scripts/finish-cask-pr-handoff.sh' "${homebrew_block}" | tail -1 | cut -d: -f1)"
# There are now two `prepared` calls — the idempotency-rerun revalidation and the final pre-promotion
# validation. The invariant guards the LATTER: the ready mutation must come after the FINAL prepared
# validation, so key on the last match.
prepared_line="$(grep -Fn -- '"$evidence" prepared' "${homebrew_block}" | tail -1 | cut -d: -f1)"
if [ -z "${ready_line}" ] || [ -z "${prepared_line}" ] || [ "${ready_line}" -le "${prepared_line}" ]; then
	fail 'guarded cask completion must come after the final prepared validation'
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
assert_contains 'git/matching-refs/heads/${branch}' \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	'the checkpoint script must inspect retained evergreen branch refs when no PR is open'
assert_contains '--force-with-lease="refs/heads/${branch}:${branch_sha}"' \
	"${repo_root}/.github/scripts/redraft-evergreen-cask-prs.sh" \
	'the checkpoint script must delete terminal branches with an exact-SHA compare-and-swap'
# The checkpoint script is release-critical: CI must both re-run this suite when it
# changes and shellcheck it (a filter/shellcheck omission lets it break unnoticed).
assert_contains "- '.github/scripts/redraft-evergreen-cask-prs.sh'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on checkpoint-script changes'
if [ "$(grep -Fc -- '.github/scripts/redraft-evergreen-cask-prs.sh' "${ci_workflow}")" -lt 2 ]; then
	fail 'CI must also shellcheck the checkpoint script, not just path-filter it'
fi
assert_contains "- '.github/scripts/redraft-evergreen-cask-prs.test.sh'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on checkpoint-test changes'
if [ "$(grep -Fc -- '.github/scripts/redraft-evergreen-cask-prs.test.sh' "${ci_workflow}")" -lt 3 ]; then
	fail 'CI must shellcheck and execute the checkpoint behavior test'
fi
assert_contains 'release-asset digest evidence is missing' \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	'the validator must require release-asset digest evidence'
assert_contains 'does not match the published digest of' \
	"${repo_root}/.github/scripts/validate-cask-pr-handoff.sh" \
	'the validator must verify each cask sha256 against the digest of the asset its url names'

assert_contains 'if: always() && needs.publish-release.result != '\''success'\''' "${cd_workflow}" \
	'a pending cask handoff must not delete a published release'
assert_contains 'cask-pr-handoff:' "${ci_workflow}" \
	'CI must include the cask PR handoff fixture job'
assert_contains '.github/scripts/validate-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must execute the handoff fixture suite'
assert_contains '.github/scripts/collect-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must execute the handoff collector suite'
assert_contains '.github/scripts/find-merged-cask-pr-handoff.test.sh' "${ci_workflow}" \
	'CI must include the paginated merged-handoff finder suite'
assert_contains "- '.github/scripts/find-merged-cask-pr-handoff.sh'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on merged-finder changes'
assert_contains "- '.github/scripts/find-merged-cask-pr-handoff.test.sh'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on merged-finder test changes'
if [[ "$(grep -Fc -- '.github/scripts/find-merged-cask-pr-handoff.sh' "${ci_workflow}" || true)" -lt 2 ]]; then
	fail 'CI must shellcheck the merged finder in addition to path-filtering it'
fi
if [[ "$(grep -Fc -- '.github/scripts/find-merged-cask-pr-handoff.test.sh' "${ci_workflow}" || true)" -lt 3 ]]; then
	fail 'CI must path-filter, shellcheck, and execute the merged-finder test'
fi
if ! grep -Eq '^[[:space:]]+\.github/scripts/find-merged-cask-pr-handoff\.test\.sh[[:space:]]*$' \
	"${ci_workflow}"; then
	fail 'CI must execute the paginated merged-handoff finder suite as a standalone command'
fi
assert_contains '.github/scripts/cask-pr-handoff-workflow.test.sh' "${ci_workflow}" \
	'CI must execute the handoff workflow contract suite'

# GoReleaser opens each cask PR with its commit message as the title, and the tap squash-merges on
# that title under a Conventional commit-message ruleset. The default message ("Brew cask update for
# ...") is rejected, so each config must set a Conventional commit_msg_template that renders to the
# exact title the handoff job normalizes to (#6975). The tap's pattern, with \w spelled for POSIX ERE:
tap_title_pattern='^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test){1}(\([[:alnum:]_.-]+\))?(!)?:'
sample_tag='v7.183.3'
for goreleaser_config in "${repo_root}/.goreleaser.yaml" "${repo_root}/.goreleaser.desktop.yaml"; do
	config_name="$(basename -- "${goreleaser_config}")"
	casks_block="${tmp_dir}/${config_name}.casks"
	awk '
		/^homebrew_casks:/ { in_casks = 1; next }
		in_casks && /^[^[:space:]#]/ { in_casks = 0 }
		in_casks { print }
	' "${goreleaser_config}" >"${casks_block}"

	cask_name="$(sed -nE 's/^  - name: *"?([^"[:space:]]+)"?[[:space:]]*$/\1/p' "${casks_block}")"
	[[ -n "${cask_name}" && "${cask_name}" != *$'\n'* ]] ||
		fail "${config_name} must define exactly one homebrew cask"
	project_name="$(sed -nE 's/^project_name: *"?([^"[:space:]]+)"?[[:space:]]*$/\1/p' \
		"${goreleaser_config}")"
	project_name="${project_name:-ksail}"

	templates="$(sed -nE 's/^    commit_msg_template: *"(.*)"[[:space:]]*$/\1/p' "${casks_block}")"
	[[ -n "${templates}" && "${templates}" != *$'\n'* ]] ||
		fail "${config_name} must set exactly one commit_msg_template on its cask (GoReleaser's default title is not Conventional)"
	[[ "${templates}" == 'chore(cask): update {{ .ProjectName }} to {{ .Tag }}' ]] ||
		fail "${config_name} commit_msg_template must be 'chore(cask): update {{ .ProjectName }} to {{ .Tag }}', got '${templates}'"

	rendered="${templates//\{\{ .ProjectName \}\}/${project_name}}"
	rendered="${rendered//\{\{ .Tag \}\}/${sample_tag}}"
	[[ "${rendered}" =~ ${tap_title_pattern} ]] ||
		fail "${config_name} cask PR title '${rendered}' does not match the tap's Conventional ruleset"
	[[ "${rendered}" == "chore(cask): update ${cask_name} to ${sample_tag}" ]] ||
		fail "${config_name} cask PR title '${rendered}' must equal the handoff's normalized title for cask ${cask_name}"
done
[[ ! 'Brew cask update for ksail version v7.183.3' =~ ${tap_title_pattern} ]] ||
	fail 'the tap title pattern must reject GoReleaser default cask titles (negative control)'
assert_contains "- '.goreleaser.yaml'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on GoReleaser config changes'
assert_contains "- '.goreleaser.desktop.yaml'" "${ci_workflow}" \
	'CI paths filter must trigger the handoff job on desktop GoReleaser config changes'

printf 'Cask PR handoff workflow contract passed.\n'
