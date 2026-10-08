#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF_USAGE'
Usage:
  report-eks-account-capacity.sh --region REGION

Print what is using EC2 launch capacity in the EKS smoke-test account.

When AWS refuses to launch a node group's instances for an account limit, the
refusal does not say whether the limit is low or whether something left behind
is using it up. This prints what could be using it: fleets, Spot fleet
requests, instances and Auto Scaling groups in the region, counted by state,
and the fleet, On-Demand and Spot launch limits when the role may read them.

It only reads and it creates nothing, so it can be run without a cluster. It
prints states, types and counts, never an identifier. A read that fails is
reported in one line and the script still exits 0. Only a usage error exits
non-zero.
EOF_USAGE
}

region=""

while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--region)
		if (($# < 2)); then
			usage >&2
			exit 2
		fi
		region="$2"
		shift 2
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "${region}" =~ ^[a-z]{2}(-[a-z]+)+-[0-9]$ ]]; then
	usage >&2
	exit 2
fi

readonly max_lines=40

# Runs one read and prints how many rows it returned, then the rows grouped and
# counted. Prints a one-line note when the read fails. Never fails the script.
# Rows hold states and types only: the queries below select no identifier.
count() {
	local title="$1" output
	shift
	if ! output="$("$@" 2>/dev/null)"; then
		printf '%s: (could not be read)\n' "${title}"
		return 0
	fi
	# The CLI prints a null result as the literal None.
	if [[ ! "${output}" =~ [^[:space:]] || "${output}" == None ]]; then
		printf '%s: 0\n' "${title}"
		return 0
	fi
	printf '%s: %s\n' "${title}" "$(grep -c . <<<"${output}" || true)"
	# awk reads to the end, so nothing closes the pipe early.
	sort <<<"${output}" | uniq -c | awk -v limit="${max_lines}" 'NR <= limit { print "  " $0 }' || true
}

printf '::group::EC2 launch capacity in use (%s)\n' "${region}"
# What follows quotes text AWS wrote. Stop the runner reading any of it as a
# workflow command until the matching token.
fence="capacity-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
printf '::stop-commands::%s\n' "${fence}"

count 'EC2 fleets, by state and type' \
	aws ec2 describe-fleets --region "${region}" \
	--query 'Fleets[].[FleetState, Type]' --output text
count 'Spot fleet requests, by state' \
	aws ec2 describe-spot-fleet-requests --region "${region}" \
	--query 'SpotFleetRequestConfigs[].[SpotFleetRequestState]' --output text
count 'Spot instance requests, by state' \
	aws ec2 describe-spot-instance-requests --region "${region}" \
	--query 'SpotInstanceRequests[].[State]' --output text
count 'Instances not terminated, by state and type' \
	aws ec2 describe-instances --region "${region}" \
	--filters 'Name=instance-state-name,Values=pending,running,stopping,stopped,shutting-down' \
	--query 'Reservations[].Instances[].[State.Name, InstanceType]' --output text
count 'Auto Scaling groups, by desired capacity' \
	aws autoscaling describe-auto-scaling-groups --region "${region}" \
	--query 'AutoScalingGroups[].[DesiredCapacity]' --output text
count 'Fleet, On-Demand and Spot limits, by name and value' \
	aws service-quotas list-service-quotas --region "${region}" --service-code ec2 \
	--query "Quotas[?contains(QuotaName, 'Fleet') || contains(QuotaName, 'On-Demand Standard') || contains(QuotaName, 'Standard (A, C, D, H, I, M, R, T, Z) Spot')].[QuotaName, Value]" \
	--output text

printf '::%s::\n' "${fence}"
printf '::endgroup::\n'
