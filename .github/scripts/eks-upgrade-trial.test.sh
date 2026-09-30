#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
trial="$script_dir/eks-upgrade-trial.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin"

cat >"$scratch/bin/curl" <<'STUB'
#!/usr/bin/env bash
printf '{"value":"fixture-oidc-token"}\n'
STUB
cat >"$scratch/bin/timeout" <<'STUB'
#!/usr/bin/env bash
shift
exec "$@"
STUB
cat >"$scratch/bin/aws" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
'sts assume-role-with-web-identity')
  [[ "$TRIAL_SCENARIO" != missing-expiry ]] || { echo '{"Credentials":{}}'; exit 0; }
  echo '{"Credentials":{"AccessKeyId":"fixture-key","SecretAccessKey":"fixture-secret","SessionToken":"fixture-session","Expiration":"2099-01-01T00:00:00Z"}}'
  ;;
'eks describe-cluster')
  version=1.34
  arn=original
  [[ ! -f "$TRIAL_STATE/upgraded" ]] || version=1.35
  [[ "$TRIAL_SCENARIO" != changed-identity || ! -f "$TRIAL_STATE/upgraded" ]] || arn=replaced
  [[ "$TRIAL_SCENARIO" != wrong-start ]] || version=1.33
  jq -n --arg version "$version" --arg arn "$arn" '{cluster:{arn:$arn,createdAt:123,status:"ACTIVE",version:$version}}'
  ;;
'eks list-updates')
  [[ "$TRIAL_SCENARIO" != inventory-error ]] || exit 1
  if [[ -f "$TRIAL_STATE/reupgraded" ]]; then echo '{"updateIds":["older","upgrade","unexpected"]}'; exit 0; fi
  if [[ -f "$TRIAL_STATE/upgraded" ]]; then
    echo '{"updateIds":["older","upgrade"]}'
  else
    echo '{"updateIds":["older"]}'
  fi
  ;;
'eks describe-update')
  status=Successful
  [[ "$TRIAL_SCENARIO" != unsuccessful-update ]] || status=Failed
  jq -n --arg status "$status" '{update:{id:"upgrade",type:"VersionUpdate",status:$status,params:[{type:"Version",value:"1.35"}]}}'
  ;;
*) exit 80 ;;
esac
STUB
cat >"$scratch/bin/ksail" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'cluster update --kubernetes-version 1.35 --yes' ]] || exit 81
if [[ -n "${AWS_SESSION_TOKEN:-}" ]]; then
  touch "$TRIAL_STATE/negative"
  case "$TRIAL_SCENARIO" in
  accepts-unknown-expiry) exit 0 ;;
  unrelated-rejection) echo 'network unavailable' >&2; exit 1 ;;
  *) echo 'EKS upgrade credentials must remain valid through the bounded wait plus one minute' >&2; exit 1 ;;
  esac
fi
[[ -f "$TRIAL_STATE/negative" ]] || exit 82
[[ "$AWS_PROFILE" == ksail-upgrade-trial ]] || exit 83
[[ -s "$AWS_CONFIG_FILE" ]] || exit 84
jq -e '.Version == 1 and .Expiration == "2099-01-01T00:00:00Z"' "$KSAIL_EKS_TRIAL_CREDENTIALS" >/dev/null || exit 85
case "$TRIAL_SCENARIO" in
upgrade-error) exit 1 ;;
silent-noop) exit 0 ;;
esac
if [[ -f "$TRIAL_STATE/upgraded" ]]; then
  touch "$TRIAL_STATE/repeated"
  [[ "$TRIAL_SCENARIO" != repeat-mutated ]] || touch "$TRIAL_STATE/reupgraded"
else
  touch "$TRIAL_STATE/upgraded"
fi
STUB
cat >"$scratch/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
if [[ "$TRIAL_SCENARIO" == unready-api ]]; then exit 1; fi
echo ok
STUB
chmod +x "$scratch/bin/"*

run_case() {
  local scenario="$1" expected="$2" state="$scratch/$1" status=0
  mkdir -p "$state/tmp" "$state/project"
  PATH="$scratch/bin:$PATH" TRIAL_SCENARIO="$scenario" TRIAL_STATE="$state" \
    RUNNER_TEMP="$state/tmp" KSAIL_EKS_WORKDIR="$state/project" \
    KSAIL_EKS_CLUSTER_NAME=st-eks-fixture AWS_REGION=us-east-1 \
    EKS_UPGRADE_FROM=1.34 EKS_UPGRADE_TO=1.35 \
    AWS_OIDC_ROLE_ARN=arn:aws:iam::123456789012:role/eks-ci \
    ACTIONS_ID_TOKEN_REQUEST_URL='https://oidc.invalid/token?scope=fixture' \
    ACTIONS_ID_TOKEN_REQUEST_TOKEN=fixture-request-token \
    bash "$trial" >"$state/output" 2>&1 || status=$?
  if [[ "$status" != "$expected" ]]; then
    cat "$state/output" >&2
    echo "FAIL: $scenario expected $expected got $status" >&2
    exit 1
  fi
  [[ -z "$(ls -A "$state/tmp")" ]] || {
    echo "FAIL: credentials survived $scenario" >&2
    exit 1
  }
  if grep -Eq 'fixture-(secret|session|oidc-token|request-token)' "$state/output"; then
    echo "FAIL: credentials leaked in $scenario" >&2
    exit 1
  fi
  if [[ "$scenario" == success ]]; then
    [[ -f "$state/repeated" ]] || {
      echo 'FAIL: repeat invocation missing' >&2
      exit 1
    }
    echo 'PASS: successful upgrade and repeat'
  else
    echo "PASS: $scenario"
  fi
}

run_case success 0
run_case accepts-unknown-expiry 1
run_case unrelated-rejection 1
run_case wrong-start 1
run_case upgrade-error 1
run_case silent-noop 1
run_case changed-identity 1
run_case unsuccessful-update 1
run_case unready-api 1
run_case missing-expiry 1
run_case inventory-error 1
run_case repeat-mutated 1

for pair in '1.34:' ':1.35' '1.34:1.34' '1.34:1.36' '1.35:1.34' '1.034:1.35'; do
  if EKS_UPGRADE_FROM="${pair%:*}" EKS_UPGRADE_TO="${pair#*:}" bash "$trial" --validate-versions >/dev/null 2>&1; then
    echo "FAIL: invalid version pair accepted: $pair" >&2
    exit 1
  fi
done
EKS_UPGRADE_FROM='' EKS_UPGRADE_TO='' bash "$trial" --validate-versions
echo 'PASS: version inputs fail closed and default off'
