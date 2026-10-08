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
# fails or is empty. Never fails the script. With order "sorted" the lines are
# ordered by their leading timestamp: events arrive newest first, and the
# earliest failure is the cause.
report() {
	local title="$1" empty_note="$2" order="$3" output
	shift 3
	printf '%s\n' "${title}"
	if ! output="$("$@" 2>/dev/null)"; then
		printf '  (could not be read)\n'
		return 0
	fi
	# The CLI prints a null result as the literal None.
	if [[ ! "${output}" =~ [^[:space:]] || "${output}" == None ]]; then
		printf '  %s\n' "${empty_note}"
		return 0
	fi
	if [[ "${order}" == sorted ]]; then
		output="$(sort -s -k1,1 <<<"${output}")" || true
	fi
	# awk reads to the end, so nothing closes the pipe early.
	awk -v limit="${max_lines}" 'NR <= limit { print "  " $0 }' <<<"${output}" || true
}

printf '::group::Why the EKS create failed (%s)\n' "${cluster_name}"
# What follows quotes text AWS wrote. Stop the runner reading any of it as a
# workflow command until the matching token.
fence="report-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
printf '::stop-commands::%s\n' "${fence}"
# The lists below are split on whitespace; never expand a returned name as a glob.
set -f

# The names come from list-stacks, not from an unscoped describe-stacks: the
# smoke-test role may describe only its own eksctl-st-eks-* stacks, so a
# describe with no stack name is denied, while listing is allowed on every
# stack.
#
# Each stack is then read by its id, not its name. A stack already deleted when
# this runs keeps its events, which may be the only record of the failure, but
# it can be read only by id. The id holds the account number, so it is passed
# to the reads and never printed; the report shows the stack's name.
stack_ids=""
if ! stack_ids="$(aws cloudformation list-stacks --region "${region}" \
	--query "StackSummaries[?starts_with(StackName, 'eksctl-${cluster_name}-')].StackId" \
	--output text 2>/dev/null)"; then
	printf 'The stack list could not be read.\n'
	stack_ids=""
fi

readonly stack_id_pattern='^arn:aws[a-z-]*:cloudformation:[a-z0-9-]+:[0-9]{12}:stack/([A-Za-z][A-Za-z0-9-]{0,127})/[0-9a-f-]{36}$'
stack_count=0
for stack_id in ${stack_ids}; do
	# An id AWS returned goes on a command line next; accept only a stack id for
	# one of this cluster's stacks.
	if [[ ! "${stack_id}" =~ ${stack_id_pattern} ]]; then
		continue
	fi
	stack="${BASH_REMATCH[1]}"
	if [[ "${stack}" != "eksctl-${cluster_name}-"* ]]; then
		continue
	fi
	stack_count=$((stack_count + 1))
	report "Stack ${stack}: status" '(no status)' plain \
		aws cloudformation describe-stacks --region "${region}" --stack-name "${stack_id}" \
		--query 'Stacks[0].[StackStatus, StackStatusReason]' --output text
	report "Stack ${stack}: failed resources, earliest first" '(no failed resource events)' sorted \
		aws cloudformation describe-stack-events --region "${region}" --stack-name "${stack_id}" \
		--query "StackEvents[?contains(ResourceStatus, 'FAILED')].[Timestamp, LogicalResourceId, ResourceType, ResourceStatus, ResourceStatusReason]" \
		--output text
done
if [[ "${stack_count}" -eq 0 ]]; then
	printf 'No stack named eksctl-%s-* was found.\n' "${cluster_name}"
fi

nodegroups=""
nodegroups_read=true
if ! nodegroups="$(aws eks list-nodegroups --region "${region}" --cluster-name "${cluster_name}" \
	--query 'nodegroups[]' --output text 2>/dev/null)"; then
	printf 'The node group list could not be read (the cluster may not exist yet).\n'
	nodegroups=""
	nodegroups_read=false
fi
nodegroup_count=0
for nodegroup in ${nodegroups}; do
	if [[ ! "${nodegroup}" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$ ]]; then
		continue
	fi
	nodegroup_count=$((nodegroup_count + 1))
	report "Node group ${nodegroup}: status" '(no status)' plain \
		aws eks describe-nodegroup --region "${region}" --cluster-name "${cluster_name}" \
		--nodegroup-name "${nodegroup}" --query 'nodegroup.status' --output text
	report "Node group ${nodegroup}: health issues" '(none reported)' plain \
		aws eks describe-nodegroup --region "${region}" --cluster-name "${cluster_name}" \
		--nodegroup-name "${nodegroup}" --query 'nodegroup.health.issues[].[code, message]' --output text
done
# A node group that failed to create is deleted by its stack's rollback, so an
# empty list is a finding, not a blank: the stack events above are then the record.
if [[ "${nodegroups_read}" == true && "${nodegroup_count}" -eq 0 ]]; then
	printf 'No node group was found for the cluster.\n'
fi

printf '::%s::\n' "${fence}"
printf '::endgroup::\n'
