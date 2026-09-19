#!/usr/bin/env bash
set -euo pipefail

action="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/action.yaml"

guard="$({
  awk '
    /- name: .*ksail workload apply/ { step = 1 }
    step && /^      run: \|/ { run = 1; next }
    run && /^    - name:/ { exit }
    run { sub(/^        /, ""); print }
  ' "$action"
} | awk '
  /# The overlay must resolve from this checkout/ { keep = 1 }
  /^apply_succeeded=false$/ { exit }
  keep
')"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

run_case() {
  local name=$1 expected=$2 resource=$3 fixture result=0
  fixture="$(mktemp -d)"
  trap 'rm -rf "$fixture"' RETURN
  printf 'resources:\n  - %s\n' "$resource" >"$fixture/kustomization.yaml"

  OVERLAY_PATH="$fixture" bash -euo pipefail -c "$guard" >/dev/null 2>&1 || result=$?
  if [ "$expected" = reject ] && [ "$result" -eq 0 ]; then
    fail "$name was accepted"
  fi
  if [ "$expected" = accept ] && [ "$result" -ne 0 ]; then
    fail "$name was rejected"
  fi
  rm -rf "$fixture"
  trap - RETURN
}

run_case local accept ./base
run_case https reject https://github.com/stefanprodan/podinfo//kustomize
run_case ssh reject ssh://git@github.com/stefanprodan/podinfo
run_case scp reject git@github.com:stefanprodan/podinfo
run_case shorthand reject github.com/stefanprodan/podinfo
run_case legacy-prefix reject git::https://github.com/stefanprodan/podinfo//kustomize

printf 'remote-resource-guard tests passed\n'
