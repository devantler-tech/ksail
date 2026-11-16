#!/usr/bin/env bash
# Read the dedicated CI project. This never deletes or classifies ownership.
# Only a complete empty project proves absence without an ownership baseline.
set -uo pipefail
umask 077

if [[ -z "${HCLOUD_TOKEN:-}" ]] || ! command -v hcloud >/dev/null || ! command -v jq >/dev/null; then
  echo 'HETZNER-INVENTORY=UNVERIFIED reason=missing-prerequisite'
  exit 2
fi

inventory_dir="$(mktemp -d)" || exit 2
trap 'rm -rf -- "${inventory_dir}"' EXIT
unverified=0
nonempty=0

# Include resources created by the provider, CCM, CSI and Talos image uploader.
# Images exclude vendor-supplied system/app images, which are not project assets.
for kind in server primary-ip floating-ip volume load-balancer network firewall placement-group ssh-key image; do
  list_args=("${kind}" list --output json)
  [[ "${kind}" != image ]] || list_args+=(--type 'snapshot,backup')
  if ! hcloud --config /dev/null --endpoint https://api.hetzner.cloud/v1 --http-timeout 20s \
    "${list_args[@]}" \
    >"${inventory_dir}/response.json" 2>"${inventory_dir}/error"; then
    echo "FAMILY ${kind} UNVERIFIED reason=list-failed"
    unverified=1
    continue
  fi
  if ! count="$(jq -ser '
    if length == 1 and (.[0] | type == "array") and
      all(.[0][]; type == "object" and
        (.id | type == "number" and . > 0 and floor == . and . <= 9007199254740991))
    then .[0] | length
    else error("invalid inventory") end
  ' "${inventory_dir}/response.json" 2>/dev/null)"; then
    echo "FAMILY ${kind} UNVERIFIED reason=invalid-response"
    unverified=1
  elif [[ "${count}" -ne 0 ]]; then
    echo "FAMILY ${kind} NONEMPTY count=${count}"
    nonempty=1
  else
    echo "FAMILY ${kind} EMPTY count=0"
  fi
done

if [[ "${unverified}" -ne 0 ]]; then
  echo 'HETZNER-INVENTORY=UNVERIFIED families=10'
  exit 2
fi
if [[ "${nonempty}" -ne 0 ]]; then
  echo 'HETZNER-INVENTORY=NONEMPTY families=10'
  exit 1
fi
echo 'HETZNER-INVENTORY=EMPTY families=10'
