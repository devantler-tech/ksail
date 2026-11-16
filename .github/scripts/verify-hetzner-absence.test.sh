#!/usr/bin/env bash
# Exercise the real inventory reader with only the remote CLI replaced.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
inventory="${HETZNER_INVENTORY_SCRIPT:-${script_dir}/verify-hetzner-absence.sh}"
test_dir="$(mktemp -d)"
trap 'rm -rf -- "${test_dir}"' EXIT
mkdir "${test_dir}/bin"
cat >"${test_dir}/bin/hcloud" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1 $2 $3 $4 $5 $6" == '--config /dev/null --endpoint https://api.hetzner.cloud/v1 --http-timeout 20s' ]] || exit 97
shift 6
kind="$1"
[[ "$2 $3 $4" == 'list --output json' ]] || exit 98
case "${kind}" in
server|primary-ip|floating-ip|volume|load-balancer|network|firewall|placement-group|ssh-key) [[ "$#" -eq 4 ]] || exit 99 ;;
image) [[ "$#" -eq 6 && "$5 $6" == '--type snapshot,backup' ]] || exit 99 ;;
*) exit 100 ;;
esac
printf '%s\n' "${kind}" >>"${TEST_CALLS}"
if [[ "${kind}" == "${TEST_FAMILY:-}" ]]; then
  case "${TEST_MODE}" in
  residual) printf '[{"id":928374,"name":"private-resource-name"}]\n' ;;
  failure) echo 'private-provider-error' >&2; exit 1 ;;
  object) printf '{"resources":[]}\n' ;;
  multiple) printf '[]\n[]\n' ;;
  empty) : ;;
  invalid-id) printf '[{"id":null}]\n' ;;
  *) exit 101 ;;
  esac
else
  printf '[]\n'
fi
FAKE
chmod +x "${test_dir}/bin/hcloud"

cases=0
run_case() {
  local name="$1" family="$2" mode="$3" want_status="$4" want_verdict="$5"
  local output status=0 calls="${test_dir}/${name}.calls"
  : >"${calls}"
  output="$(PATH="${test_dir}/bin:${PATH}" HCLOUD_TOKEN=fixture-token \
    TEST_CALLS="${calls}" TEST_FAMILY="${family}" TEST_MODE="${mode}" \
    bash "${inventory}" 2>&1)" || status=$?
  if [[ "${status}" -ne "${want_status}" || "${output}" != *"HETZNER-INVENTORY=${want_verdict} families=10"* ]]; then
    printf 'FAIL %s: expected status=%s verdict=%s; got status=%s\n%s\n' \
      "${name}" "${want_status}" "${want_verdict}" "${status}" "${output}" >&2
    exit 1
  fi
  # Every independent family is read exactly once even after a failed earlier read.
  if [[ "$(sort "${calls}" | uniq | wc -l | tr -d ' ')" -ne 10 || "$(wc -l <"${calls}" | tr -d ' ')" -ne 10 ]]; then
    echo "FAIL ${name}: incomplete family coverage" >&2
    exit 1
  fi
  if [[ "${output}" == *private-* || "${output}" == *928374* || "${output}" == *fixture-token* ]]; then
    echo "FAIL ${name}: provider data reached the log" >&2
    exit 1
  fi
  cases=$((cases + 1))
}

# Omitting any family would turn its residual case into a false empty verdict.
for family in server primary-ip floating-ip volume load-balancer network firewall placement-group ssh-key image; do
  run_case "residual-${family}" "${family}" residual 1 NONEMPTY
done
run_case empty-project '' '' 0 EMPTY
for mode in failure object multiple empty invalid-id; do
  run_case "unverified-${mode}" server "${mode}" 2 UNVERIFIED
done

echo "PASS: ${cases} Hetzner absence cases"
