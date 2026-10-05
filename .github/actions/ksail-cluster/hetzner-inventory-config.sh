#!/usr/bin/env bash
# Select bounded initial capacity only for the one-node inventory acceptance trial.
set -euo pipefail

[[ "${DISTRIBUTION:-}" == Talos && "${PROVIDER:-}" == Hetzner && "${INIT:-}" == true ]] || {
  echo 'ERROR: inventory capacity requires initialized Talos on Hetzner' >&2
  exit 1
}
case "${TRIAL_SERVER_TYPE:-}" in
cx23 | cx33) ;;
*)
  echo 'ERROR: inventory capacity permits only cx23 or cx33' >&2
  exit 1
  ;;
esac
case "${TRIAL_LOCATION:-}" in
fsn1 | nbg1 | hel1) ;;
*)
  echo 'ERROR: inventory capacity requires an approved European location' >&2
  exit 1
  ;;
esac

config="${CONFIG:-ksail.yaml}"
yq -e '.spec.cluster.distribution == "Talos" and
  .spec.cluster.provider == "Hetzner" and
  (.spec.cluster.controlPlanes // 1) == 1 and
  (.spec.cluster.workers // 0) == 0' "$config" >/dev/null

# Init must already have run: it overwrites a prewritten config. Both types matter
# because provider admission checks the configured worker type even with no workers.
yq -i '.spec.provider.hetzner.controlPlaneServerType = strenv(TRIAL_SERVER_TYPE) |
  .spec.provider.hetzner.workerServerType = strenv(TRIAL_SERVER_TYPE) |
  .spec.provider.hetzner.location = strenv(TRIAL_LOCATION)' "$config"
