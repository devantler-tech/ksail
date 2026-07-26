#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cleaner="${script_dir}/delete-eks-smoke-cluster.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
workdir="${tmp_dir}/workdir"
pass_count=0

mkdir -p "${fake_bin}" "${workdir}"

cat >"${fake_bin}/aws" <<'EOF'
#!/usr/bin/env bash
case "${FAKE_AWS_DESCRIBE_MODE:-not-found}" in
found)
	echo 'ACTIVE'
	exit 0
	;;
deleting)
	echo 'DELETING'
	exit 0
	;;
until-fallback)
	# Present until eksctl runs, so the eksctl path is only reached if the
	# script probes AWS rather than trusting ksail's exit status.
	if [[ ! -f "${FAKE_STATE_DIR}/eksctl-ran" ]]; then
		echo 'ACTIVE'
		exit 0
	fi
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found for name: st-eks-1-1.' >&2
	exit 254
	;;
denied)
	echo 'An error occurred (AccessDeniedException) when calling the DescribeCluster operation: not authorized' >&2
	exit 254
	;;
*)
	echo 'An error occurred (ResourceNotFoundException) when calling the DescribeCluster operation: No cluster found for name: st-eks-1-1.' >&2
	exit 254
	;;
esac
EOF

cat >"${fake_bin}/ksail" <<'EOF'
#!/usr/bin/env bash
exit "${FAKE_KSAIL_DELETE_STATUS:-0}"
EOF

cat >"${fake_bin}/eksctl" <<'EOF'
#!/usr/bin/env bash
touch "${FAKE_STATE_DIR}/eksctl-ran"
exit "${FAKE_EKSCTL_DELETE_STATUS:-0}"
EOF

chmod +x "${fake_bin}/aws" "${fake_bin}/ksail" "${fake_bin}/eksctl"

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
	local output status state_dir="${tmp_dir}/state-${scenario}"

	mkdir -p "${state_dir}"

	set +e
	output="$(PATH="${fake_bin}:${PATH}" \
		FAKE_AWS_DESCRIBE_MODE="${describe_mode}" \
		FAKE_KSAIL_DELETE_STATUS="${ksail_status}" \
		FAKE_EKSCTL_DELETE_STATUS="${eksctl_status}" \
		FAKE_STATE_DIR="${state_dir}" \
		"${cleaner}" \
		--cluster-name st-eks-1-1 \
		--region us-east-1 \
		--workdir "${case_workdir}" \
		--create-attempted "${attempted}" 2>&1)"
	status=$?
	set -e

	expect_status "${scenario}" "${expected_status}" "${status}" "${output}" || return 1
	expect_substring "${scenario}" "${expected_output}" "${output}" || return 1

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

# ksail fails, the eksctl fallback succeeds, and absence is confirmed.
run_case deleted-by-eksctl-fallback 0 'No cluster st-eks-1-1 remains' not-found 1 0 "${workdir}"

# The case that must stay red: every delete "succeeded" but the cluster is still there, so it is
# still accruing cost and a human has to look.
run_case still-present 1 'may be billable' found 0 0 "${workdir}"

# EKS teardown is asynchronous, so a delete that has taken effect can still report DELETING for a
# while. That is not a leak, and calling it one would just move the false alarm from failed creates
# onto successful deletes.
run_case deleting-in-progress 0 'already tearing down' deleting 0 0 "${workdir}"

# Fail closed. A probe that cannot prove absence (denied, throttled, unreachable) must never be
# reported as a clean teardown, because that is what strands a billable cluster silently.
run_case probe-inconclusive 1 'Could not determine whether cluster' denied 0 0 "${workdir}"

# A zero exit from ksail does not prove deletion. AWS keeps reporting the cluster until eksctl
# actually runs, so this only passes if the fallback is driven by the probe rather than by ksail's
# status — otherwise cleanup spends one of its two teardown attempts and then gives up.
run_case fallback-after-silent-ksail-noop 0 'No cluster st-eks-1-1 remains' until-fallback 0 0 "${workdir}"

# ksail needs the scaffolded project directory but eksctl does not, so a missing workdir must not
# skip verification: a cluster can still be running with no local trace of it.
run_case missing-workdir-still-present 1 'may be billable' found 0 0 "${tmp_dir}/absent"

# Same path, nothing actually left — verified rather than assumed.
run_case missing-workdir-verified-clean 0 'No cluster st-eks-1-1 remains' not-found 0 0 "${tmp_dir}/absent"

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
