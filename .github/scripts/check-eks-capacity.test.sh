#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir "$fixture/bin" "$fixture/work"
go build -o "$fixture/capacity" "$repo_root/internal/maintenance/eksinventory/capacitycmd"
export CAPACITY_BINARY="$fixture/capacity" FIXTURE_ROOT="$fixture"
cat >"$fixture/bin/go" <<'EOF_GO'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == run && "$2" == ./internal/maintenance/eksinventory/capacitycmd ]] || exit 99
shift 2
exec "$CAPACITY_BINARY" "$@"
EOF_GO
cat >"$fixture/bin/aws" <<'EOF_AWS'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 10 && "$3" == --region && "$4" == eu-central-1 && "$5" == --output && "$6" == json && "$7" == --no-paginate && "$8" == --cli-input-json && "${10}" == --no-cli-pager ]] || exit 99
printf '%s %s\n' "$1" "$2" >> "$FIXTURE_ROOT/calls"
case "$1 $2" in
 'sts get-caller-identity') printf '%s\n' '{"Account":"123456789012"}' ;;
 'service-quotas list-service-quotas')
  if [[ "$FIXTURE_MODE" == denied ]]; then
   printf '%s\n' '{"Quotas":[{"QuotaCode":"L-1216C47A","Value":999}]}'
   printf '%s\n' '(AccessDenied) private-account ::error::injected' >&2
   exit 1
  fi
  if [[ "$FIXTURE_MODE" == malformed ]]; then printf '%s\n' '{"Quotas":null}'; exit 0; fi
  limit=4
  [[ "$FIXTURE_MODE" != insufficient ]] || limit=0
  [[ "$FIXTURE_MODE" != frankfurt-smoke ]] || limit=16
  [[ "$FIXTURE_MODE" != above-budget ]] || limit=100
  printf '{"Quotas":[{"QuotaCode":"L-1216C47A","Value":%s}]}\n' "$limit" ;;
 'ec2 describe-instances')
  [[ "$9" == *'"IncludeManagedResources":true'* && "$9" != *'"Filters"'* ]] || exit 99
  if [[ "$FIXTURE_MODE" == later-denial ]]; then
   if [[ "$9" == *'NextToken'* ]]; then printf '%s\n' '{"Reservations":[]}'; exit 1; fi
   printf '%s\n' '{"Reservations":[],"NextToken":"private-page-token"}'
  else printf '%s\n' '{"Reservations":[]}'; fi ;;
 'ec2 describe-capacity-reservations') printf '%s\n' '{"CapacityReservations":[]}' ;;
 'ec2 describe-instance-types') printf '%s\n' '{"InstanceTypes":[{"InstanceType":"t3.medium","VCpuInfo":{"DefaultVCpus":2}}]}' ;;
 *) exit 99 ;;
esac
EOF_AWS
chmod +x "$fixture/bin/go" "$fixture/bin/aws"
export PATH="$fixture/bin:$PATH"
plan='{"apiVersion":"eksctl.io/v1alpha5","kind":"ClusterConfig","metadata":{"name":"fixture","region":"eu-central-1"},"managedNodeGroups":[{"name":"default","instanceType":"t3.medium","desiredCapacity":1,"minSize":1,"maxSize":1}]}'
cd "$fixture/work"
for mode in admitted frankfurt-smoke above-budget insufficient denied malformed later-denial; do
	export FIXTURE_MODE="$mode"
	peak=1
	[[ "$mode" != frankfurt-smoke ]] || peak=2
	[[ "$mode" != above-budget ]] || peak=8
	status=0
	"$script_dir/check-eks-capacity.sh" --region eu-central-1 --cluster-name fixture --peak-nodes "$peak" <<<"$plan" >"$fixture/output" 2>&1 || status=$?
	if [[ "$mode" == admitted ]]; then
		[[ "$status" == 0 ]]
		grep -q 'admitted quota=4 usage=0 pending-buffer=0 target=2 surge=2 required=4' "$fixture/output"
	elif [[ "$mode" == frankfurt-smoke ]]; then
		[[ "$status" == 0 ]]
		grep -q 'admitted quota=16 usage=0 pending-buffer=0 target=4 surge=2 required=6 trial-ceiling=16' "$fixture/output"
	else
		[[ "$status" != 0 ]]
		if [[ "$mode" == insufficient ]]; then
			grep -q 'refused (insufficient-headroom)' "$fixture/output"
		elif [[ "$mode" == above-budget ]]; then
			grep -q 'refused (trial-ceiling) quota=100 usage=0 pending-buffer=0 target=16 surge=2 required=18 trial-ceiling=16' "$fixture/output"
		else grep -q 'unknown' "$fixture/output"; fi
	fi
	if grep -Eq '123456789012|private-account|private-page-token|::error::injected' "$fixture/output"; then
		echo "Provider details leaked" >&2
		exit 1
	fi
done
rm "$fixture/calls"
"$script_dir/check-eks-capacity.sh" --help >/dev/null
status=0
"$script_dir/check-eks-capacity.sh" --region 'eu-central-1;injected' --cluster-name fixture --peak-nodes 1 >/dev/null 2>&1 || status=$?
[[ "$status" == 2 && ! -e "$fixture/calls" ]]
echo 'PASS capacity entrypoint: Frankfurt smoke and budget, exact headroom, insufficient quota, denied/malformed/partial reads, privacy and usage'
