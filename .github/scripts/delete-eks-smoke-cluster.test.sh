#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cleaner="${script_dir}/delete-eks-smoke-cluster.sh"
tmp_dir="$(mktemp -d)"
unenterable_workdir="${tmp_dir}/unenterable"
# The unenterable case is created without search permission, so restore it before
# the recursive delete or the trap cannot clean up after itself.
trap 'chmod 700 "${unenterable_workdir}" 2>/dev/null || true; rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
workdir="${tmp_dir}/workdir"
pass_count=0

# Use a real process deadline, shortened only at the external timeout boundary.
# The cleaner itself keeps its production budgets; the outer guard catches a
# missing deadline without leaving a hung fixture behind.
real_timeout="$(command -v timeout || command -v gtimeout)"

mkdir -p "${fake_bin}" "${workdir}" "${unenterable_workdir}"
chmod 000 "${unenterable_workdir}"

# A directory the process cannot enter is the whole point of the cases below, and
# root can enter one regardless of its mode. Prove the precondition holds rather
# than letting those cases pass without exercising anything.
if (cd "${unenterable_workdir}") 2>/dev/null; then
	printf 'FAIL: harness precondition — %s is enterable, so the unenterable-workdir cases prove nothing (running as root?).\n' \
		"${unenterable_workdir}" >&2
	exit 1
fi

cat >"${fake_bin}/aws" <<'EOF'
#!/usr/bin/env bash
printf 'probe\n' >> "${FAKE_STATE_DIR}/probes"
if [[ "${FAKE_AWS_DESCRIBE_MODE:-}" == hung ||
	("${FAKE_AWS_DESCRIBE_MODE:-}" == hung-until-fallback && ! -f "${FAKE_STATE_DIR}/eksctl-ran") ]]; then
	exec sleep 60
fi
if [[ "${FAKE_AWS_DESCRIBE_MODE:-}" == hung-not-found ]]; then
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found' >&2
	exec sleep 60
fi
case "${FAKE_AWS_DESCRIBE_MODE:-not-found}" in
found)
	echo 'ACTIVE'
	exit 0
	;;
deleting)
	echo 'DELETING'
	exit 0
	;;
until-fallback | deleting-until-fallback | hung-until-fallback)
	# Present until eksctl runs, so the eksctl path is only reached if the
	# script probes AWS rather than trusting ksail's exit status.
	if [[ ! -f "${FAKE_STATE_DIR}/eksctl-ran" ]]; then
		if [[ "${FAKE_AWS_DESCRIBE_MODE}" == "deleting-until-fallback" ]]; then
			echo 'DELETING'
		else
			echo 'ACTIVE'
		fi
		exit 0
	fi
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found for name: st-eks-1-1.' >&2
	exit 254
	;;
denied)
	echo 'An error occurred (AccessDeniedException) when calling the DescribeCluster operation: not authorized' >&2
	exit 254
	;;
partial-deleting-error)
	echo 'DELETING'
	echo 'An error occurred (AccessDeniedException) when calling the DescribeCluster operation: not authorized' >&2
	exit 254
	;;
profile-not-found-token)
	echo 'The config profile (ResourceNotFoundException) could not be found' >&2
	exit 255
	;;
wrong-operation-not-found)
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeNodegroup operation: No nodegroup found' >&2
	exit 254
	;;
not-found-leading-newline)
	printf '\nAn error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found\n' >&2
	exit 254
	;;
not-found-cli-prefix)
	printf '\naws: [ERROR]: An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found\n' >&2
	exit 254
	;;
partial-not-found-leading-newline)
	printf '\nDELETING\n\nAn error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found\n' >&2
	exit 254
	;;
profile-cli-prefix)
	printf '\naws: [ERROR]: The config profile (ResourceNotFoundException) could not be found\n' >&2
	exit 255
	;;
partial-not-found)
	echo 'DELETING'
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found' >&2
	exit 254
	;;
empty)
	exit 0
	;;
*)
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found for name: st-eks-1-1.' >&2
	exit 254
	;;
esac
EOF

cat >"${fake_bin}/ksail" <<'EOF'
#!/usr/bin/env bash
[[ "${FAKE_KSAIL_DELETE_STATUS:-0}" != hung ]] || exec sleep 60
exit "${FAKE_KSAIL_DELETE_STATUS:-0}"
EOF

cat >"${fake_bin}/eksctl" <<'EOF'
#!/usr/bin/env bash
touch "${FAKE_STATE_DIR}/eksctl-ran"
printf '%s\n' "$*" > "${FAKE_STATE_DIR}/eksctl-args"
[[ "${FAKE_EKSCTL_DELETE_STATUS:-0}" != hung ]] || exec sleep 60
exit "${FAKE_EKSCTL_DELETE_STATUS:-0}"
EOF

cat >"${fake_bin}/timeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" =~ ^--kill-after=([1-9][0-9]*)s$ ]] || exit 99
grace="${BASH_REMATCH[1]}"
shift
[[ "$1" =~ ^([1-9][0-9]*)([sm])$ ]] || exit 99
seconds="${BASH_REMATCH[1]}"
[[ "${BASH_REMATCH[2]}" != m ]] || seconds=$((seconds * 60))
shift
printf '%s\n' "$((seconds + grace))" >> "${FAKE_STATE_DIR}/budgets"
exec "${REAL_TIMEOUT}" --kill-after=0.1s 1s "$@"
EOF

chmod +x "${fake_bin}/aws" "${fake_bin}/ksail" "${fake_bin}/eksctl" "${fake_bin}/timeout"

expect_status() {
	[[ "$3" == "$2" ]] && return 0
	printf 'FAIL: %s\n  want exit %s, got %s\n  output:\n%s\n' "$1" "$2" "$3" "$4" >&2
	return 1
}

expect_substring() {
	[[ "$3" == *"$2"* ]] && return 0
	printf 'FAIL: %s\n  want output containing: %s\n  output:\n%s\n' "$1" "$2" "$3" >&2
	return 1
}

run_case() {
	local scenario="$1" expected_status="$2" expected_output="$3"
	local describe_mode="$4" ksail_status="$5" eksctl_status="$6" case_workdir="$7"
	local attempted="${8:-true}"
	local expected_fallback="${9:-}"
	local output status state_dir="${tmp_dir}/state-${scenario}"

	mkdir -p "${state_dir}"

	set +e
	output="$(PATH="${fake_bin}:${PATH}" \
		FAKE_AWS_DESCRIBE_MODE="${describe_mode}" \
		FAKE_KSAIL_DELETE_STATUS="${ksail_status}" \
		FAKE_EKSCTL_DELETE_STATUS="${eksctl_status}" \
		FAKE_STATE_DIR="${state_dir}" \
		REAL_TIMEOUT="${real_timeout}" \
		"${real_timeout}" --kill-after=1s 10s \
		"${cleaner}" \
		--cluster-name st-eks-1-1 \
		--region us-east-1 \
		--workdir "${case_workdir}" \
		--create-attempted "${attempted}" 2>&1)"
	status=$?
	set -e

	expect_status "${scenario}" "${expected_status}" "${status}" "${output}" || return 1
	expect_substring "${scenario}" "${expected_output}" "${output}" || return 1
	if [[ "${expected_status}" == "1" && "${output}" == *"No cluster st-eks-1-1 remains"* ]]; then
		printf 'FAIL: %s claimed absence despite incomplete cleanup.\n' "${scenario}" >&2
		return 1
	fi
	if [[ "${expected_fallback}" == "yes" ]]; then
		if [[ ! -f "${state_dir}/eksctl-ran" ]]; then
			printf 'FAIL: %s skipped the waiting fallback.\n' "${scenario}" >&2
			return 1
		fi
		expect_substring "${scenario}" '--wait' "$(cat "${state_dir}/eksctl-args")" || return 1
	fi
	if [[ "${scenario}" == hung-* ]]; then
		local budget total=0
		while read -r budget; do total=$((total + budget)); done <"${state_dir}/budgets"
		if ((total >= 45 * 60)) || [[ "$(wc -l <"${state_dir}/probes" | tr -d ' ')" != 2 ]]; then
			printf 'FAIL: %s did not reserve time for fallback and final absence verification.\n' "${scenario}" >&2
			return 1
		fi
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${scenario}"
}

# Regression for the failure seen in run 29822971789: `ksail cluster create` died before any
# cluster existed, so both delete paths returned not-found and the step failed the job. Nothing
# was left running, so cleanup must report success — otherwise a red cleanup no longer means
# "a billable cluster may still be up".
run_case nothing-to-clean 0 'No cluster st-eks-1-1 remains' not-found 1 1 "${workdir}"

# The ordinary success path: ksail tears the cluster down and AWS confirms it is gone.
run_case deleted-by-ksail 0 'No cluster st-eks-1-1 remains' not-found 0 0 "${workdir}"
run_case aws-cli-leading-newline 0 'No cluster st-eks-1-1 remains' not-found-leading-newline 0 0 "${workdir}"
run_case aws-cli-error-prefix 0 'No cluster st-eks-1-1 remains' not-found-cli-prefix 0 0 "${workdir}"

# ksail fails, the eksctl fallback succeeds, and absence is confirmed.
run_case deleted-by-eksctl-fallback 0 'No cluster st-eks-1-1 remains' until-fallback 1 0 "${workdir}" true yes

# The case that must stay red: every delete "succeeded" but the cluster is still there, so it is
# still accruing cost and a human has to look.
run_case still-present 1 'may be billable' found 0 0 "${workdir}"

# A deleting cluster still exists. The waiting fallback must run, and an unchanged
# post-condition remains a failure even when both delete commands report success.
run_case deleting-still-present 1 'may be billable' deleting 0 0 "${workdir}" true yes
run_case deleting-until-fallback 0 'No cluster st-eks-1-1 remains' deleting-until-fallback 0 0 "${workdir}" true yes
run_case deleting-with-failed-fallback 1 'may be billable' deleting 0 1 "${workdir}" true yes

# Fail closed. A probe that cannot prove absence (denied, throttled, unreachable) must never be
# reported as a clean teardown, because that is what strands a billable cluster silently.
run_case probe-inconclusive 1 'Could not determine whether cluster' denied 0 0 "${workdir}"
run_case partial-deleting-probe 1 'Could not determine whether cluster' partial-deleting-error 0 0 "${workdir}" true yes
run_case unrelated-not-found-token 1 'Could not determine whether cluster' profile-not-found-token 0 0 "${workdir}" true yes
run_case wrong-operation-not-found 1 'Could not determine whether cluster' wrong-operation-not-found 0 0 "${workdir}" true yes
run_case partial-not-found 1 'Could not determine whether cluster' partial-not-found 0 0 "${workdir}" true yes
run_case partial-not-found-leading-newline 1 'Could not determine whether cluster' partial-not-found-leading-newline 0 0 "${workdir}" true yes
run_case unrelated-cli-prefix 1 'Could not determine whether cluster' profile-cli-prefix 0 0 "${workdir}" true yes
run_case empty-probe 1 'may be billable' empty 0 0 "${workdir}" true yes

# A zero exit from ksail does not prove deletion. AWS keeps reporting the cluster until eksctl
# actually runs, so this only passes if the fallback is driven by the probe rather than by ksail's
# status — otherwise cleanup spends one of its two teardown attempts and then gives up.
run_case fallback-after-silent-ksail-noop 0 'No cluster st-eks-1-1 remains' until-fallback 0 0 "${workdir}"

# A stalled command must not consume the workflow's whole cleanup window. The
# actual deadline kills it, then fallback and the explicit final probe still run.
run_case hung-primary-delete 0 'No cluster st-eks-1-1 remains' until-fallback hung 0 "${workdir}" true yes
run_case hung-fallback-delete 1 'may be billable' found 1 hung "${workdir}" true yes
run_case hung-initial-probe 0 'No cluster st-eks-1-1 remains' hung-until-fallback 0 0 "${workdir}" true yes
run_case hung-final-probe 1 'Could not determine whether cluster' hung 0 0 "${workdir}" true yes
run_case hung-not-found-output 1 'Could not determine whether cluster' hung-not-found 0 0 "${workdir}" true yes

# ksail needs the scaffolded project directory but eksctl does not, so a missing workdir must not
# skip verification: a cluster can still be running with no local trace of it.
run_case missing-workdir-still-present 1 'may be billable' found 0 0 "${tmp_dir}/absent"

# Same path, nothing actually left — verified rather than assumed.
run_case missing-workdir-verified-clean 0 'No cluster st-eks-1-1 remains' not-found 0 0 "${tmp_dir}/absent"

# A workdir that exists but cannot be entered reaches the same place as a missing one: ksail is
# unusable, eksctl and the probe are not. Under strict mode an unguarded `cd` would end the script
# on the spot, so the cluster would still be running and nothing would say so.
run_case unenterable-workdir-still-present 1 'may be billable' found 0 0 "${unenterable_workdir}"

# And the clean variant, so the guard cannot be satisfied by failing everything.
run_case unenterable-workdir-verified-clean 0 'No cluster st-eks-1-1 remains' not-found 0 0 "${unenterable_workdir}"

# A typo in the creation-attempt flag must not read as "nothing was created". Silently skipping
# teardown is precisely the failure this script exists to prevent.
run_case invalid-create-attempted 2 'must be exactly true or false' not-found 0 0 "${workdir}" maybe

set +e
skipped_output="$(PATH="${fake_bin}:${PATH}" "${cleaner}" \
	--cluster-name st-eks-1-1 \
	--region us-east-1 \
	--workdir "${workdir}" \
	--create-attempted false 2>&1)"
skipped_status=$?
set -e
if [[ "${skipped_status}" -ne 0 || "${skipped_output}" != *"Cluster creation did not start"* ]]; then
	printf 'FAIL: create-not-attempted: expected clean skip, got status %s:\n%s\n' \
		"${skipped_status}" "${skipped_output}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: create-not-attempted\n'

set +e
"${cleaner}" --cluster-name st-eks-1-1 --region us-east-1 >/dev/null 2>&1
missing_arg_status=$?
set -e
if [[ "${missing_arg_status}" -ne 2 ]]; then
	printf 'FAIL: missing-required-args: expected status 2, got %s\n' "${missing_arg_status}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: missing-required-args\n'

printf '\n%s cases passed.\n' "${pass_count}"
