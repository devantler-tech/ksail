#!/usr/bin/env bash
# Exercise the real inventory reader with only the remote transport replaced.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
inventory="${HETZNER_INVENTORY_SCRIPT:-${script_dir}/verify-hetzner-absence.sh}"
test_dir="$(mktemp -d)"
trap 'rm -rf -- "${test_dir}"' EXIT
mkdir "${test_dir}/bin"
# The former CLI boundary normalizes a missing/null provider list to []. Keep
# this double so the regression fails with EMPTY on the old implementation.
cat >"${test_dir}/bin/hcloud" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
shift 6
printf '%s\n' "$1" >>"${TEST_CALLS}"
printf '[]\n'
FAKE
cat >"${test_dir}/bin/curl" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == --disable ]] || exit 97
shift
output='' url='' page='' types='' deprecated='' get=0 bounded=0 header=0 per_page=0 writeout='' code=200
trap '[[ -z "$writeout" ]] || printf "%s" "$code"' EXIT
while [[ "$#" -gt 0 ]]; do
  case "$1" in
  --fail|--silent|--show-error) shift ;;
  --proto) [[ "$2" == '=https' ]] || exit 97; shift 2 ;;
  --tlsv1.2) shift ;;
  --connect-timeout) [[ "$2" == 10 ]] || exit 97; shift 2 ;;
  --max-time) [[ "$2" == 20 ]] || exit 97; bounded=1; shift 2 ;;
  --header) [[ "$2" == @- ]] || exit 97; header=1; shift 2 ;;
  --get) get=1; shift ;;
  --output) output="$2"; shift 2 ;;
  --write-out) [[ "$2" == '%{http_code}' ]] || exit 97; writeout=1; shift 2 ;;
  --url) url="$2"; shift 2 ;;
  --data-urlencode)
    case "$2" in
    page=*) page="${2#page=}" ;;
    per_page=50) per_page=1 ;;
    type=snapshot|type=backup) types+=" $2" ;;
    include_deprecated=true) deprecated=1 ;;
    *) exit 98 ;;
    esac
    shift 2 ;;
  *) exit 99 ;;
  esac
done
[[ "$get $bounded $header $per_page" == '1 1 1 1' && "$page" =~ ^[1-9][0-9]*$ && -n "$output" ]] || exit 99
[[ "$(cat)" == 'Authorization: Bearer fixture-token' ]] || exit 99
case "$url" in
https://api.hetzner.cloud/v1/servers) kind=server; key=servers ;;
https://api.hetzner.cloud/v1/primary_ips) kind=primary-ip; key=primary_ips ;;
https://api.hetzner.cloud/v1/floating_ips) kind=floating-ip; key=floating_ips ;;
https://api.hetzner.cloud/v1/volumes) kind=volume; key=volumes ;;
https://api.hetzner.cloud/v1/load_balancers) kind=load-balancer; key=load_balancers ;;
https://api.hetzner.cloud/v1/networks) kind=network; key=networks ;;
https://api.hetzner.cloud/v1/firewalls) kind=firewall; key=firewalls ;;
https://api.hetzner.cloud/v1/placement_groups) kind=placement-group; key=placement_groups ;;
https://api.hetzner.cloud/v1/ssh_keys) kind=ssh-key; key=ssh_keys ;;
https://api.hetzner.cloud/v1/images) kind=image; key=images ;;
*) exit 99 ;;
esac
if [[ "$kind" == image ]]; then
  [[ "$types $deprecated" == ' type=snapshot type=backup 1' ]] || exit 99
else
  [[ -z "$types$deprecated" ]] || exit 99
fi
printf '%s %s\n' "$kind" "$page" >>"${TEST_CALLS}"
items='[]' next=null last=1 total=0
if [[ "$kind" == "${TEST_FAMILY:-}" ]]; then
  case "$TEST_MODE" in
  null-list) printf '{"%s":null,"meta":{"pagination":{"page":1,"per_page":50,"next_page":null,"last_page":1,"total_entries":0}}}\n' "$key" >"$output"; exit ;;
  missing-list) printf '{"meta":{"pagination":{"page":1,"per_page":50,"next_page":null,"last_page":1,"total_entries":0}}}\n' >"$output"; exit ;;
  residual) items='[{"id":928374,"name":"private-resource-name"}]'; total=1 ;;
  failure) echo 'private-provider-error' >&2; exit 22 ;;
  object) items='{}' ;;
  multiple) printf '{}\n{}\n' >"$output"; exit ;;
  empty) : >"$output"; exit ;;
  invalid-id) items='[{"id":null}]'; total=1 ;;
  missing-pagination) printf '{"%s":[]}\n' "$key" >"$output"; exit ;;
  wrong-page) page=2 ;;
  wrong-total) total=1 ;;
  redirect-with-json) code=302 ;;
  pagination)
    items="[{\"id\":$((928373 + page)),\"name\":\"private-resource-name\"}]"; last=2; total=2
    [[ "$page" -ne 1 ]] || next=2 ;;
  second-page-null)
    if [[ "$page" -eq 1 ]]; then items='[{"id":928374}]'; next=2; last=2; total=2; else items=null; last=2; total=2; fi ;;
  duplicate-page)
    items='[{"id":928374}]'; last=2; total=2
    [[ "$page" -ne 1 ]] || next=2 ;;
  cyclic-page) next=1; last=null; total=null ;;
  unknown-total) last=null; total=null ;;
  *) exit 101 ;;
  esac
fi
printf '{"%s":%s,"meta":{"pagination":{"page":%s,"per_page":50,"next_page":%s,"last_page":%s,"total_entries":%s}}}\n' \
  "$key" "$items" "$page" "$next" "$last" "$total" >"$output"
FAKE
chmod +x "${test_dir}/bin/hcloud" "${test_dir}/bin/curl"

cases=0
run_case() {
	local name="$1" family="$2" mode="$3" want_status="$4" want_verdict="$5"
	local output status=0 calls="${test_dir}/${name}.calls" want_calls="${6:-10}"
	: >"${calls}"
	output="$(PATH="${test_dir}/bin:${PATH}" HCLOUD_TOKEN=fixture-token \
		TEST_CALLS="${calls}" TEST_FAMILY="${family}" TEST_MODE="${mode}" \
		bash "${inventory}" 2>&1)" || status=$?
	if [[ "${status}" -ne "${want_status}" || "${output}" != *"HETZNER-INVENTORY=${want_verdict} families=10"* ]]; then
		printf 'FAIL %s: expected status=%s verdict=%s; got status=%s\n%s\n' \
			"${name}" "${want_status}" "${want_verdict}" "${status}" "${output}" >&2
		exit 1
	fi
	# A failed family never suppresses the other nine independent reads.
	if [[ "$(cut -d ' ' -f 1 "${calls}" | sort -u | wc -l | tr -d ' ')" -ne 10 || "$(wc -l <"${calls}" | tr -d ' ')" -ne "${want_calls}" ]]; then
		echo "FAIL ${name}: incomplete family coverage" >&2
		exit 1
	fi
	if [[ "${output}" == *private-* || "${output}" == *928374* || "${output}" == *fixture-token* ]]; then
		echo "FAIL ${name}: provider data reached the log" >&2
		exit 1
	fi
	cases=$((cases + 1))
}

run_case null-provider-list server null-list 2 UNVERIFIED
run_case missing-provider-list server missing-list 2 UNVERIFIED
run_case redirect-with-valid-body server redirect-with-json 2 UNVERIFIED
# Omitting any family would turn its residual case into a false empty verdict.
for family in server primary-ip floating-ip volume load-balancer network firewall placement-group ssh-key image; do
	run_case "residual-${family}" "${family}" residual 1 NONEMPTY
done
run_case empty-project '' '' 0 EMPTY
for mode in failure object multiple empty invalid-id missing-pagination wrong-page wrong-total cyclic-page; do
	run_case "unverified-${mode}" server "${mode}" 2 UNVERIFIED
done
run_case paginated-residual server pagination 1 NONEMPTY 11
run_case malformed-second-page server second-page-null 2 UNVERIFIED 11
run_case duplicated-page server duplicate-page 2 UNVERIFIED 11
run_case empty-with-unknown-total server unknown-total 0 EMPTY

echo "PASS: ${cases} Hetzner absence cases"
