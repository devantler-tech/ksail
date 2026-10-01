#!/usr/bin/env bash
set -euo pipefail

: "${UPGRADE_FROM:?missing starting Kubernetes version}"
: "${UPGRADE_TO:?missing target Kubernetes version}"
log_dir=${SYSTEM_TEST_LOG_DIR:-/tmp/ksail-system-test-logs}
mkdir -p "$log_dir"

assert_running_version() {
  local expected=$1 phase=$2 actual
  actual=$(kubectl version --request-timeout=30s -o json | jq -er '.serverVersion.gitVersion')
  if [[ "$actual" != "$expected" ]]; then
    echo "❌ ERROR: $phase server version is $actual; expected $expected"
    return 1
  fi
  if ! kubectl get nodes --request-timeout=30s -o json | jq -e --arg version "$expected" '
    (.items | length) > 0 and all(.items[];
      .status.nodeInfo.kubeletVersion == $version and
      any(.status.conditions[]; .type == "Ready" and .status == "True"))' >/dev/null; then
    echo "❌ ERROR: $phase node versions or readiness do not match $expected"
    return 1
  fi
}

assert_running_version "$UPGRADE_FROM" "before upgrade"
echo "🧪 Explicit Talos Kubernetes upgrade: $UPGRADE_FROM → $UPGRADE_TO"

# Creation pins the source; the upgrade and repeat both pin the target. Preserve
# other update arguments while removing init-only and source-version options.
update_args_text=$(printf '%s' "${ARGS:-}" | sed -E 's/--image-verification [^ ]*//g; s/--kubernetes-version(=| +)[^ ]*//g')
read -r -a update_args <<< "$update_args_text"
if ! timeout --kill-after=10s 900s ksail cluster update --force --distribution Talos --provider Docker \
  "${update_args[@]}" --kubernetes-version "$UPGRADE_TO" 2>&1 | tee "$log_dir/talos-kubernetes-upgrade.log"; then
  echo "❌ ERROR: explicit Talos Kubernetes upgrade failed"
  exit 1
fi
assert_running_version "$UPGRADE_TO" "after upgrade"

stdout_file="$log_dir/talos-upgrade-repeat.stdout"
stderr_file="$log_dir/talos-upgrade-repeat.stderr"
if ! timeout --kill-after=10s 900s ksail cluster update --force --output json --distribution Talos --provider Docker \
  "${update_args[@]}" --kubernetes-version "$UPGRADE_TO" >"$stdout_file" 2>"$stderr_file"; then
  echo "❌ ERROR: repeated update failed after the explicit Talos Kubernetes upgrade"
  cat "$stderr_file"
  exit 1
fi
if ! jq -e -s 'length == 1 and (.[0] | type == "object" and .totalChanges == 0)' "$stdout_file" >/dev/null ||
   ! grep -q "No changes detected" "$stderr_file"; then
  echo "❌ ERROR: repeated update did not report one JSON document with zero changes"
  exit 1
fi
if grep -qE 'Kubernetes upgraded|Kubernetes upgrade path|upgrading Kubernetes|Would upgrade Kubernetes' "$stderr_file"; then
  echo "❌ ERROR: repeated update performed a Kubernetes upgrade"
  exit 1
fi
assert_running_version "$UPGRADE_TO" "after repeated update"
echo "✅ Explicit Kubernetes upgrade and repeated no-op passed: $UPGRADE_FROM → $UPGRADE_TO"
