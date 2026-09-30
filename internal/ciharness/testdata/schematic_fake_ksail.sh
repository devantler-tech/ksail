#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$CALLS_FILE"
if [[ "$1 $2" == "cluster update" ]]; then
  if [[ " $* " == *" --dry-run "* ]]; then
    count=0
    [[ ! -f "$DRY_COUNT_FILE" ]] || count=$(<"$DRY_COUNT_FILE")
    count=$((count + 1))
    printf '%s' "$count" > "$DRY_COUNT_FILE"
    if [[ "$SCENARIO" == pre-missing && "$count" == 1 ]] ||
       [[ "$SCENARIO" == converged && "$count" == 2 ]]; then
      echo 'No changes detected'
    else
      echo 'Would reconcile distribution image at v1.12.4.'
      if [[ "$count" == 1 ]]; then
        if [[ "$SCENARIO" == pre-config-drift ]]; then
          echo 'Would apply 1 in-place, 0 reboot-required, 0 recreate-required'
        else
          echo 'No changes detected'
        fi
      fi
    fi
  else
    echo 'Distribution image reconciled at v1.12.4.'
  fi
elif [[ "$1 $2" == "cluster info" ]]; then
  echo 'Ready: 1/1 (ready/total)'
elif [[ "$1 $2 $3" == "workload get nodes" ]]; then
  if [[ "$SCENARIO" == not-ready ]]; then
    echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"False"}]}}]}'
  else
    echo '{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}'
  fi
else
  exit 1
fi
