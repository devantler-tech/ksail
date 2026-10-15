#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
reporter="${script_dir}/report-eks-account-capacity.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
fake_dir="${tmp_dir}/responses"
pass_count=0
mkdir -p "${fake_bin}" "${fake_dir}"

cat >"${fake_bin}/aws" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_DIR}/calls.log"
case "$1 $2" in
'eks list-clusters'|'cloudformation list-stacks'|'ec2 describe-fleets'|\
'ec2 describe-spot-fleet-requests'|'ec2 describe-spot-instance-requests'|\
'ec2 describe-instances'|'autoscaling describe-auto-scaling-groups'|\
'ec2 describe-nat-gateways'|'ec2 describe-addresses'|\
'ec2 describe-network-interfaces'|'ec2 describe-volumes'|\
'elb describe-load-balancers'|'elbv2 describe-load-balancers'|\
'service-quotas list-service-quotas') ;;
*) touch "${FAKE_DIR}/UNEXPECTED_CALL"; exit 1 ;;
esac
key="$1-$2"
cat "${FAKE_DIR}/${key}.json"
if [[ -f "${FAKE_DIR}/${key}.fail" ]]; then
	echo 'An error occurred (AccessDenied) when calling the operation: private-account private-resource' >&2
	exit 254
fi
FAKE
chmod +x "${fake_bin}/aws"

printf '{"clusters":[]}\n' >"${fake_dir}/eks-list-clusters.json"
printf '{"StackSummaries":[]}\n' >"${fake_dir}/cloudformation-list-stacks.json"
printf '{"Fleets":[]}\n' >"${fake_dir}/ec2-describe-fleets.json"
printf '{"SpotFleetRequestConfigs":[]}\n' >"${fake_dir}/ec2-describe-spot-fleet-requests.json"
printf '{"SpotInstanceRequests":[]}\n' >"${fake_dir}/ec2-describe-spot-instance-requests.json"
printf '{"Reservations":[]}\n' >"${fake_dir}/ec2-describe-instances.json"
printf '{"AutoScalingGroups":[]}\n' >"${fake_dir}/autoscaling-describe-auto-scaling-groups.json"
printf '{"NatGateways":[]}\n' >"${fake_dir}/ec2-describe-nat-gateways.json"
printf '{"Addresses":[]}\n' >"${fake_dir}/ec2-describe-addresses.json"
printf '{"NetworkInterfaces":[]}\n' >"${fake_dir}/ec2-describe-network-interfaces.json"
printf '{"Volumes":[{"VolumeId":"private-volume","State":"available"}]}\n' >"${fake_dir}/ec2-describe-volumes.json"
printf '{"LoadBalancerDescriptions":[]}\n' >"${fake_dir}/elb-describe-load-balancers.json"
printf '{"LoadBalancers":[]}\n' >"${fake_dir}/elbv2-describe-load-balancers.json"
printf '{"Quotas":[{"QuotaCode":"L-1216C47A","Value":5},{"QuotaCode":"L-34B43A08","Value":0}]}\n' >"${fake_dir}/service-quotas-list-service-quotas.json"

# shellcheck source=eks-report-test-lib.sh
source "${script_dir}/eks-report-test-lib.sh"

# Exercise the real entry point from outside the repository.
cd "${tmp_dir}"
run --region eu-central-1
expect_status 0 'the real report must run outside its repository directory'
expect_text 'EBS volumes, by state: 1' 'detached volumes must be counted'
expect_text '1 available' 'provider states must be rendered safely'
expect_text 'NAT gateways, by state: 0' 'a completed empty collection must be zero'
expect_text 'Classic load balancers: 0' 'both load-balancer APIs must be read'
expect_text 'ELBv2 load balancers, by state: 0' 'both load-balancer APIs must be read'
expect_text 'Running On-Demand Standard vCPUs = 5' 'readable quota values must remain available'
expect_text 'EC2 instant fleets: unknown' 'regional fleet coverage must remain qualified'
expect_text "not proof of a run's cleanup" 'regional counts cannot certify teardown'
refute_text 'private-' 'resource identifiers must never be printed'
[[ ! -e "${fake_dir}/UNEXPECTED_CALL" ]] || fail 'only allow-listed AWS reads may run'
[[ "$(wc -l <"${fake_dir}/calls.log" | tr -d ' ')" -eq 14 ]] || fail 'all fourteen read operations must run'
if grep -Fv -- '--region eu-central-1 --output json --no-paginate --cli-input-json ' "${fake_dir}/calls.log" >/dev/null; then
  fail 'every call must bind the requested region and explicit JSON pagination'
fi
expect_fenced_group 'EKS regional resources (eu-central-1)' capacity
pass 'the installed entry point reads every family and prints safe summaries'

touch "${fake_dir}/ec2-describe-volumes.fail"
run --region eu-central-1
expect_status 0 'a denied read remains diagnostic'
expect_text 'EBS volumes, by state: unknown' 'plausible stdout before a failed exit is not a complete read'
expect_text 'unknown (access-denied)' 'the report must expose a sanitized failure reason'
refute_text 'EBS volumes, by state: 1' 'a partial failed read must not retain its count'
refute_text 'private-' 'stdout and stderr from failed reads must stay private'
pass 'a failed process cannot convert plausible stdout into a completed count'
rm "${fake_dir}/ec2-describe-volumes.fail"

for body in 'null' 'None' '' '{"Volumes":null}' '{"Volumes":[{"VolumeId":"private-volume","State":"::error::private-injection"}]}'; do
  printf '%s' "${body}" >"${fake_dir}/ec2-describe-volumes.json"
  run --region eu-central-1
  expect_status 0 'malformed provider responses remain diagnostic'
  expect_text 'EBS volumes, by state: unknown' 'null, missing or hostile data is unknown'
  refute_text 'private-' 'hostile provider text must not reach public output'
done
pass 'null, empty and hostile provider responses are never zero'

for args in '' '--region' '--region eu-central-1;id' '--region EU-CENTRAL-1' '--cluster x' '--region eu-central-1 extra'; do
  rm -f "${fake_dir}/calls.log"
  # shellcheck disable=SC2086 # Deliberately split these literal argument fixtures.
  run ${args}
  expect_status 2 "usage error expected for: ${args}"
  [[ ! -e "${fake_dir}/calls.log" ]] || fail 'invalid arguments must not reach AWS'
done
pass 'invalid arguments fail before AWS access'
run --help
expect_status 0 '--help must succeed'
expect_text 'It only reads and it creates nothing' 'help must describe the read-only contract'
pass 'help works without provider access'
printf '%s checks passed\n' "${pass_count}"
