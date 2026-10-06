#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF_USAGE'
Usage:
  report-eks-create-failure.sh --cluster-name NAME --region REGION

Print why an EKS smoke-test cluster failed to create.

When a create fails, eksctl reports that a stack did not finish and points at
the CloudFormation console. The reason itself — the failed resource and what
AWS said about it — is only in the stack's events and in the node group's
health issues, and both are gone once the cleanup step deletes the cluster. This
prints them first, so the run's own log names its cause.

It only reads, and it is diagnostic: a read that fails is reported in one line
and the script still exits 0, so it can never hide the create failure or stand
between that failure and the cleanup. Only a usage error exits non-zero.
EOF_USAGE
}

cluster_name=""
region=""

while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--cluster-name)
		if (($# < 2)); then
			usage >&2
			exit 2
		fi
		cluster_name="$2"
		shift 2
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

# Both values are placed inside JMESPath and on command lines, so they must be
# plain identifiers before they are used.
if [[ ! "${cluster_name}" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,99}$ ]] ||
	[[ ! "${region}" =~ ^[a-z]{2}(-[a-z]+)+-[0-9]$ ]]; then
	usage >&2
	exit 2
fi

readonly max_lines=40

# Runs one read and prints its output indented, or a one-line note when it
# fails or is empty. Never fails the script.
report() {
	local title="$1" empty_note="$2" output
	shift 2
	printf '%s\n' "${title}"
	if ! output="$("$@" 2>/dev/null)"; then
		printf '  (could not be read)\n'
		return 0
	fi
	if [[ -z "${output//[[:space:]]/}" ]]; then
		printf '  %s\n' "${empty_note}"
		return 0
	fi
	# Events arrive newest first; the earliest failure is the cause, so sort by
	# the leading timestamp. Lines without one keep their order.
	sort -s -k1,1 <<<"${output}" | head -n "${max_lines}" | sed 's/^/  /'
}

printf '::group::Why the EKS create failed (%s)\n' "${cluster_name}"

stacks=""
if ! stacks="$(aws cloudformation describe-stacks --region "${region}" \
	--query "Stacks[?starts_with(StackName, 'eksctl-${cluster_name}-')].StackName" \
	--output text 2>/dev/null)"; then
	printf 'The stack list could not be read.\n'
	stacks=""
fi

stack_count=0
for stack in ${stacks}; do
	# A name AWS returned goes on a command line next; keep to stack-name characters.
	if [[ ! "${stack}" =~ ^[A-Za-z][A-Za-z0-9-]{0,127}$ ]]; then
		continue
	fi
	stack_count=$((stack_count + 1))
	report "Stack ${stack}: status" '(no status)' \
		aws cloudformation describe-stacks --region "${region}" --stack-name "${stack}" \
		--query 'Stacks[0].[StackStatus, StackStatusReason]' --output text
	report "Stack ${stack}: failed resources, earliest first" '(no failed resource events)' \
		aws cloudformation describe-stack-events --region "${region}" --stack-name "${stack}" \
		--query "StackEvents[?contains(ResourceStatus, 'FAILED')].[Timestamp, LogicalResourceId, ResourceType, ResourceStatus, ResourceStatusReason]" \
		--output text
done
if [[ "${stack_count}" -eq 0 ]]; then
	printf 'No stack named eksctl-%s-* was found.\n' "${cluster_name}"
fi

nodegroups=""
if ! nodegroups="$(aws eks list-nodegroups --region "${region}" --cluster-name "${cluster_name}" \
	--query 'nodegroups[]' --output text 2>/dev/null)"; then
	printf 'The node group list could not be read (the cluster may not exist yet).\n'
	nodegroups=""
fi
for nodegroup in ${nodegroups}; do
	if [[ ! "${nodegroup}" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$ ]]; then
		continue
	fi
	report "Node group ${nodegroup}: status" '(no status)' \
		aws eks describe-nodegroup --region "${region}" --cluster-name "${cluster_name}" \
		--nodegroup-name "${nodegroup}" --query 'nodegroup.status' --output text
	report "Node group ${nodegroup}: health issues" '(none reported)' \
		aws eks describe-nodegroup --region "${region}" --cluster-name "${cluster_name}" \
		--nodegroup-name "${nodegroup}" --query 'nodegroup.health.issues[].[code, message]' --output text
done

printf '::endgroup::\n'
