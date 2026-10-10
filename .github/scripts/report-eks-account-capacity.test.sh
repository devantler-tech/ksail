#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
reporter="${script_dir}/report-eks-account-capacity.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
pass_count=0

mkdir -p "${fake_bin}"

# Fake aws. Every call is logged. Answers come from files named after the call
# in ${FAKE_DIR}; a missing file is a failed call, as an AccessDenied would be.
# Any call that is not one of the six reads is recorded as unexpected.
cat >"${fake_bin}/aws" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_DIR}/calls.log"
case "$1 $2" in
'ec2 describe-fleets') key="fleets" ;;
'ec2 describe-spot-fleet-requests') key="spot-fleets" ;;
'ec2 describe-spot-instance-requests') key="spot-instances" ;;
'ec2 describe-instances') key="instances" ;;
'autoscaling describe-auto-scaling-groups') key="groups" ;;
'service-quotas list-service-quotas') key="quotas" ;;
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

reset() {
	fake_dir="${tmp_dir}/case-${pass_count}-${RANDOM}"
	mkdir -p "${fake_dir}"
	printf 'active\tmaintain\nactive\tmaintain\ndeleted_terminating\tinstant\n' >"${fake_dir}/fleets"
	printf 'cancelled_running\n' >"${fake_dir}/spot-fleets"
	: >"${fake_dir}/spot-instances"
	printf 'running\tm5.large\nrunning\tm5.large\nstopped\tt3.micro\n' >"${fake_dir}/instances"
	printf '2\n' >"${fake_dir}/groups"
	printf '%s\t%s\n' \
		'Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances' '5.0' \
		'All Standard (A, C, D, H, I, M, R, T, Z) Spot Instance Requests' '0.0' \
		>"${fake_dir}/quotas"
}

# shellcheck source=eks-report-test-lib.sh
source "${script_dir}/eks-report-test-lib.sh"

# --- what is in use is counted and grouped ------------------------------------
reset
run --region us-east-1
expect_status 0 'a full report must exit 0'
[[ ! -e "${fake_dir}/UNEXPECTED_CALL" ]] || fail 'only the six expected reads may be issued'
expect_text 'EC2 fleets, by state and type: 3' 'the fleet total must be printed'
expect_text '2 active	maintain' 'equal rows must be grouped and counted'
expect_text '1 deleted_terminating	instant' 'every state must be listed'
expect_text 'Spot fleet requests, by state: 1' 'Spot fleet requests must be counted'
expect_text 'Spot instance requests, by state: 0' 'an empty list must read as zero'
expect_text 'Instances not terminated, by state and type: 3' 'instances must be counted'
expect_text '2 running	m5.large' 'instances must be grouped by state and type'
expect_text 'Auto Scaling groups, by desired capacity: 1' 'Auto Scaling groups must be counted'
expect_text 'Fleet, On-Demand and Spot limits, by name and value: 2' 'readable limits must be printed'
expect_text '5.0' 'the limit value must be printed'
expect_text 'Spot Instance Requests	0.0' 'the Spot launch limit must be printed'
[[ "$(wc -l <"${fake_dir}/calls.log" | tr -d ' ')" -eq 6 ]] || fail 'exactly six reads must be issued'
if grep -Ev '^(ec2 describe-(fleets|spot-fleet-requests|spot-instance-requests|instances)|autoscaling describe-auto-scaling-groups|service-quotas list-service-quotas) ' \
	"${fake_dir}/calls.log" >/dev/null; then
	fail 'only describe and list calls may be issued'
fi
if grep -Fv -- '--region us-east-1 ' "${fake_dir}/calls.log" >/dev/null; then
	fail 'every read must name the requested region'
fi
# The queries decide what is read. None may select an identifier, so they are pinned.
for query in \
	'Fleets[].[FleetState, Type]' \
	'SpotFleetRequestConfigs[].[SpotFleetRequestState]' \
	'SpotInstanceRequests[].[State]' \
	'Reservations[].Instances[].[State.Name, InstanceType]' \
	'AutoScalingGroups[].[DesiredCapacity]' \
	"Quotas[?contains(QuotaName, 'Fleet') || contains(QuotaName, 'On-Demand Standard') || contains(QuotaName, 'Standard (A, C, D, H, I, M, R, T, Z) Spot')].[QuotaName, Value]"; do
	grep -Fq -- "--query ${query} --output text" "${fake_dir}/calls.log" ||
		fail "the read must use the query: ${query}"
done
grep -Fq -- '--filters Name=instance-state-name,Values=pending,running,stopping,stopped,shutting-down ' "${fake_dir}/calls.log" ||
	fail 'terminated instances must be left out'
expect_fenced_group 'EC2 launch capacity in use (us-east-1)' capacity
pass 'counts and groups what is using launch capacity'

# --- diagnostic only: a refused read never fails the script -------------------
reset
rm "${fake_dir}/quotas" "${fake_dir}/fleets"
run --region us-east-1
expect_status 0 'a refused read must still exit 0'
expect_text 'EC2 fleets, by state and type: (could not be read)' 'a refused read must be said'
expect_text 'Fleet, On-Demand and Spot limits, by name and value: (could not be read)' 'refused limits must be said'
expect_text 'Instances not terminated, by state and type: 3' 'readable sections must still be printed'
refute_text 'not authorized' "the provider's error text must not be printed"
refute_text '000000000000' "the provider's error text must not be printed"
pass 'a refused read is reported in one line and exits 0'

# --- an unreadable section must never read as an empty one --------------------
reset
rm "${fake_dir}/spot-fleets"
run --region us-east-1
refute_text 'Spot fleet requests, by state: 0' 'a refused read must not be shown as zero'
pass 'a refused read is not shown as zero'

reset
printf 'None\n' >"${fake_dir}/fleets"
run --region us-east-1
expect_status 0 'a null result must exit 0'
expect_text 'EC2 fleets, by state and type: 0' 'a null result must read as zero'
pass 'a null result is stated as zero'

# --- text from AWS cannot issue workflow commands ------------------------------
reset
printf '::error::injected\tmaintain\n' >"${fake_dir}/fleets"
run --region us-east-1
expect_status 0 'hostile text must not fail the report'
injected_line="$(grep -n '::error::injected' <<<"${output}" | head -n 1 | cut -d: -f1)"
stop_line="$(grep -n '^::stop-commands::' <<<"${output}" | head -n 1 | cut -d: -f1)"
[[ -n "${injected_line}" && -n "${stop_line}" && "${stop_line}" -lt "${injected_line}" ]] ||
	fail 'provider text must only appear after commands are stopped'
[[ "$(sed -n "${injected_line}p" <<<"${output}")" == '  '* ]] ||
	fail 'provider text must be indented, never at the start of a line'
pass 'provider text is fenced and indented'

# --- long lists are capped ------------------------------------------------------
reset
for i in $(seq 1 60); do printf 'running\ttype-%s\n' "${i}"; done >"${fake_dir}/instances"
run --region us-east-1
expect_text 'Instances not terminated, by state and type: 60' 'the total must count every row'
[[ "$(grep -c 'running	type-' <<<"${output}")" -eq 40 ]] || fail 'the grouped list must be capped at 40 lines'
pass 'a long list keeps its total and caps its lines'

# --- usage errors ----------------------------------------------------------------
for args in '' '--region' '--region us-east-1;id' '--region US-EAST-1' '--cluster x' '--region us-east-1 extra'; do
	fake_dir="${tmp_dir}/usage-${RANDOM}"
	mkdir -p "${fake_dir}"
	# shellcheck disable=SC2086 # the cases are split into arguments on purpose
	run ${args}
	expect_status 2 "usage error expected for: ${args}"
	[[ ! -e "${fake_dir}/calls.log" ]] || fail "no read may be issued for: ${args}"
done
pass 'bad arguments exit 2 before any read'

run --help
expect_status 0 '--help must exit 0'
expect_text 'It only reads and it creates nothing' '--help must print the usage text'
pass '--help prints usage'

printf '%s checks passed\n' "${pass_count}"
