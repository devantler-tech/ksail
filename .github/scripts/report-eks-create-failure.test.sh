#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
reporter="${script_dir}/report-eks-create-failure.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
pass_count=0

mkdir -p "${fake_bin}"

# Fake aws. Every call is logged. Answers come from files named after the call
# in ${FAKE_DIR}; a missing file is a failed call, as an AccessDenied would be.
# Like the smoke-test role, it lets every stack be listed but only a named stack
# be described: a describe-stacks with no stack name is always denied.
cat >"${fake_bin}/aws" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_DIR}/calls.log"
case "$1 $2" in
'cloudformation list-stacks') key="stacks" ;;
'cloudformation describe-stacks')
	if [[ "$*" == *'--stack-name '* ]]; then
		key="stack-status"
	else
		key="denied-unscoped-describe"
	fi
	;;
'cloudformation describe-stack-events') key="events" ;;
'eks list-nodegroups') key="nodegroups" ;;
'eks describe-nodegroup')
	if [[ "$*" == *'health.issues'* ]]; then
		key="nodegroup-issues"
	else
		key="nodegroup-status"
	fi
	;;
*)
	touch "${FAKE_DIR}/UNEXPECTED_CALL"
	exit 1
	;;
esac
if [[ ! -f "${FAKE_DIR}/${key}" ]]; then
	echo 'An error occurred (AccessDenied) when calling the operation: arn:aws:iam::000000000000:role/x is not authorized' >&2
	exit 254
fi
cat "${FAKE_DIR}/${key}"
FAKE
chmod +x "${fake_bin}/aws"

fake_dir=""
output=""
status=0

reset() {
	fake_dir="${tmp_dir}/case-${pass_count}-${RANDOM}"
	mkdir -p "${fake_dir}"
	printf 'eksctl-st-eks-1-1-cluster\teksctl-st-eks-1-1-nodegroup-default\n' >"${fake_dir}/stacks"
	printf 'ROLLBACK_COMPLETE\tThe following resource(s) failed to create: [ManagedNodeGroup].\n' >"${fake_dir}/stack-status"
	# Newest first, as AWS returns them.
	printf '%s\n' \
		'2026-10-06T17:37:00Z	eksctl-st-eks-1-1-nodegroup-default	AWS::CloudFormation::Stack	ROLLBACK_FAILED	late consequence' \
		'2026-10-06T17:36:00Z	ManagedNodeGroup	AWS::EKS::Nodegroup	CREATE_FAILED	NodeCreationFailure: Instances failed to join the kubernetes cluster' \
		>"${fake_dir}/events"
	printf 'default\n' >"${fake_dir}/nodegroups"
	printf 'CREATE_FAILED\n' >"${fake_dir}/nodegroup-status"
	printf 'NodeCreationFailure\tInstances failed to join the kubernetes cluster\n' >"${fake_dir}/nodegroup-issues"
}

run() {
	status=0
	output="$(FAKE_DIR="${fake_dir}" PATH="${fake_bin}:${PATH}" "${reporter}" "$@" 2>&1)" || status=$?
}

fail() {
	printf 'FAIL: %s\n--- output (status %s) ---\n%s\n---\n' "$1" "${status}" "${output}" >&2
	exit 1
}

expect_status() {
	[[ "${status}" -eq "$1" ]] || fail "$2 (expected status $1)"
}

expect_text() {
	grep -Fq -- "$1" <<<"${output}" || fail "$2"
}

refute_text() {
	if grep -Fq -- "$1" <<<"${output}"; then
		fail "$2"
	fi
}

pass() {
	pass_count=$((pass_count + 1))
	printf 'ok %s\n' "$1"
}

# --- the cause is printed, earliest first ------------------------------------
reset
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'a full report must exit 0'
[[ ! -e "${fake_dir}/UNEXPECTED_CALL" ]] || fail 'only the five expected reads may be issued'
expect_text 'NodeCreationFailure: Instances failed to join the kubernetes cluster' 'the failed resource reason must be printed'
expect_text 'Stack eksctl-st-eks-1-1-nodegroup-default: failed resources, earliest first' 'each stack must be reported'
expect_text 'Stack eksctl-st-eks-1-1-cluster: status' 'the cluster stack must be reported too'
expect_text 'ROLLBACK_COMPLETE' 'the stack status must be printed'
expect_text 'Node group default: health issues' 'node group health must be reported'
cause_line="$(grep -n 'CREATE_FAILED	NodeCreationFailure' <<<"${output}" | head -n 1 | cut -d: -f1)"
late_line="$(grep -n 'late consequence' <<<"${output}" | head -n 1 | cut -d: -f1)"
[[ -n "${cause_line}" && -n "${late_line}" && "${cause_line}" -lt "${late_line}" ]] ||
	fail 'the earliest failure must be printed before later ones'
grep -q "starts_with(StackName, 'eksctl-st-eks-1-1-')" "${fake_dir}/calls.log" ||
	fail 'the stack list must be limited to this cluster'
if grep -Ev '^(cloudformation (list-stacks|describe-stacks|describe-stack-events)|eks (list-nodegroups|describe-nodegroup)) ' \
	"${fake_dir}/calls.log" >/dev/null; then
	fail 'only describe and list calls may be issued'
fi
# The role may describe only its own named stacks, so a describe with no stack
# name is denied and would leave the whole report empty.
if grep -E '^cloudformation describe-stacks ' "${fake_dir}/calls.log" | grep -Fv -- '--stack-name ' >/dev/null; then
	fail 'stacks must be described by name only'
fi
refute_text 'The stack list could not be read.' 'the stack list must be readable with the role the smoke test has'
# The queries decide what is read and in which column order, so they are pinned.
for query in \
	"StackSummaries[?starts_with(StackName, 'eksctl-st-eks-1-1-') && StackStatus != 'DELETE_COMPLETE'].StackName" \
	"StackEvents[?contains(ResourceStatus, 'FAILED')].[Timestamp, LogicalResourceId, ResourceType, ResourceStatus, ResourceStatusReason]" \
	'Stacks[0].[StackStatus, StackStatusReason]' \
	'nodegroup.status' \
	'nodegroup.health.issues[].[code, message]' \
	'nodegroups[]'; do
	grep -Fq -- "--query ${query} --output text" "${fake_dir}/calls.log" ||
		fail "the read must use the query: ${query}"
done
[[ "$(head -n 1 <<<"${output}")" == '::group::Why the EKS create failed (st-eks-1-1)' ]] ||
	fail 'the report must open a log group'
fence="$(sed -n '2s/^::stop-commands::\(report-[0-9a-f]\{32\}\)$/\1/p' <<<"${output}")"
[[ -n "${fence}" ]] || fail 'quoted provider text must be fenced off from workflow commands'
[[ "$(tail -n 2 <<<"${output}" | head -n 1)" == "::${fence}::" ]] ||
	fail 'the command fence must be closed with its own token'
[[ "$(tail -n 1 <<<"${output}")" == '::endgroup::' ]] || fail 'the report must close its log group'
pass 'prints stack failures earliest first and node group health'

# --- diagnostic only: failed reads never fail the script ---------------------
reset
rm "${fake_dir}/stacks" "${fake_dir}/nodegroups"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'unreadable lists must still exit 0'
expect_text 'The stack list could not be read.' 'an unreadable stack list must be said'
expect_text 'The node group list could not be read' 'an unreadable node group list must be said'
refute_text 'not authorized' "the provider's error text must not be printed"
refute_text '000000000000' "the provider's error text must not be printed"
pass 'unreadable lists are reported in one line and exit 0'

reset
rm "${fake_dir}/events" "${fake_dir}/nodegroup-issues"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'unreadable details must still exit 0'
expect_text '(could not be read)' 'an unreadable detail must be said'
expect_text 'ROLLBACK_COMPLETE' 'readable details must still be printed beside unreadable ones'
pass 'an unreadable detail does not hide the readable ones'

reset
: >"${fake_dir}/events"
: >"${fake_dir}/nodegroup-issues"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'empty details must exit 0'
expect_text '(no failed resource events)' 'an empty event list must be said'
expect_text '(none reported)' 'an empty issue list must be said'
pass 'empty results are stated, not left blank'

reset
printf 'None\n' >"${fake_dir}/nodegroup-issues"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'a null result must exit 0'
expect_text '(none reported)' 'a null issue list must read as none reported'
pass 'a null result is stated as empty'

# Only events are ordered by time; other reports keep the order AWS gave.
reset
printf 'Zeta\tfirst issue\nAlpha\tsecond issue\n' >"${fake_dir}/nodegroup-issues"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'two issues must exit 0'
zeta_line="$(grep -n 'first issue' <<<"${output}" | cut -d: -f1)"
alpha_line="$(grep -n 'second issue' <<<"${output}" | cut -d: -f1)"
[[ "${zeta_line}" -lt "${alpha_line}" ]] || fail 'health issues must keep their given order'
pass 'reports other than events keep their order'

reset
: >"${fake_dir}/stacks"
: >"${fake_dir}/nodegroups"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'no stacks must exit 0'
expect_text 'No stack named eksctl-st-eks-1-1-* was found.' 'a missing stack must be said'
expect_text 'No node group was found for the cluster.' 'a missing node group must be said'
pass 'a create that failed before any stack existed says so'

# A failed node group is deleted by its stack's rollback before the report runs.
reset
: >"${fake_dir}/nodegroups"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'a rolled-back node group must exit 0'
expect_text 'NodeCreationFailure: Instances failed to join the kubernetes cluster' 'the stack events must still name the cause'
expect_text 'No node group was found for the cluster.' 'an empty node group list must be said, not left blank'
pass 'a node group already rolled back is stated and the stack events still explain it'

reset
rm "${fake_dir}/nodegroups"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'an unreadable node group list must exit 0'
refute_text 'No node group was found for the cluster.' 'an unreadable list must not be reported as an empty one'
pass 'an unreadable node group list is not reported as empty'

# --- names AWS returns are checked before they reach a command line ----------
reset
printf 'eksctl-st-eks-1-1-cluster\t--endpoint-url=http://x\n' >"${fake_dir}/stacks"
printf 'default\t--profile=x\n' >"${fake_dir}/nodegroups"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'an odd returned name must not fail the script'
if grep -Eq -- '--stack-name --|--nodegroup-name --' "${fake_dir}/calls.log"; then
	fail 'a returned name that looks like an option must not be passed on'
fi
pass 'a returned name that is not a plain name is skipped'

# --- long output is bounded ---------------------------------------------------
reset
for index in $(seq 1 200); do
	printf '2026-10-06T17:%02d:00Z\tR%s\tAWS::X\tCREATE_FAILED\treason\n' "$((index % 60))" "${index}"
done >"${fake_dir}/events"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'long output must exit 0'
[[ "$(grep -c 'CREATE_FAILED	reason' <<<"${output}")" -eq 80 ]] ||
	fail 'each stack report must be capped at 40 lines (two stacks here)'
pass 'each report is capped'

# Far more than a pipe holds: the cap must not end the script through a closed pipe.
reset
for index in $(seq 1 4000); do
	printf '2026-10-06T17:%02d:00Z\tResource%s\tAWS::EKS::Nodegroup\tCREATE_FAILED\t%0120d\n' "$((index % 60))" "${index}" 0
done >"${fake_dir}/events"
run --cluster-name st-eks-1-1 --region us-east-1
expect_status 0 'very long output must still exit 0'
expect_text 'Node group default: health issues' 'the reports after a very long one must still be printed'
[[ "$(tail -n 1 <<<"${output}")" == '::endgroup::' ]] || fail 'a very long report must still close its log group'
pass 'output larger than a pipe buffer is capped without ending the script'

# --- usage --------------------------------------------------------------------
usage_case() {
	local label="$1"
	shift
	reset
	run "$@"
	expect_status 2 "${label} must be a usage error"
	[[ ! -e "${fake_dir}/calls.log" ]] || fail "${label}: a usage error must not call AWS"
	pass "${label} is a usage error before any call"
}
usage_case 'no arguments'
usage_case 'a missing region' --cluster-name st-eks-1-1
usage_case 'a flag with no value' --cluster-name st-eks-1-1 --region
usage_case 'an unknown flag' --cluster-name st-eks-1-1 --region us-east-1 --extra
usage_case 'a cluster name with a quote' --cluster-name "x') || true" --region us-east-1
usage_case 'a region that is not a region' --cluster-name st-eks-1-1 --region 'us-east-1 --debug'

printf '\n%d case(s) passed.\n' "${pass_count}"
