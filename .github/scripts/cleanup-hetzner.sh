#!/usr/bin/env bash
set -euo pipefail

# Cleanup script for Hetzner Cloud resources created by KSail CI tests.
# Deletes all resources labeled with ksail.owned=true to ensure clean slate
# for next CI run, even if individual test jobs fail to clean up after themselves.

echo "🧹 Starting Hetzner Cloud cleanup for KSail-owned resources..."

# Check if HCLOUD_TOKEN is set
if [[ -z "${HCLOUD_TOKEN:-}" ]]; then
  echo "⚠️  HCLOUD_TOKEN not set, skipping cleanup"
  exit 0
fi

# Install hcloud CLI if not already available
if ! command -v hcloud &> /dev/null; then
  echo "📦 Installing hcloud CLI..."
  curl -sL https://github.com/hetznercloud/cli/releases/download/v1.49.0/hcloud-linux-amd64.tar.gz | tar -xz -C /tmp
  sudo mv /tmp/hcloud /usr/local/bin/hcloud
  chmod +x /usr/local/bin/hcloud
fi

# Configure hcloud context
if ! hcloud context create cleanup-context --token="${HCLOUD_TOKEN}" 2>/dev/null; then
  # Context might already exist, try to use it
  echo "Context already exists or creation failed, attempting to use existing context..."
fi
if ! hcloud context use cleanup-context; then
  echo "❌ Failed to configure hcloud context"
  exit 1
fi

LABEL_SELECTOR="ksail.owned=true"

echo "🔍 Finding KSail-owned resources with label: ${LABEL_SELECTOR}"

# Delete all KSail-owned servers
echo "🗑️  Deleting servers..."
SERVER_IDS=$(hcloud server list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>/dev/null || true)
if [[ -n "${SERVER_IDS}" ]]; then
  echo "Found servers to delete:"
  hcloud server list -l "${LABEL_SELECTOR}"
  for SERVER_ID in ${SERVER_IDS}; do
    echo "  Deleting server ID: ${SERVER_ID}"
    hcloud server delete "${SERVER_ID}" || echo "⚠️  Failed to delete server ${SERVER_ID}"
  done
else
  echo "  No servers found"
fi

# Small delay to let server deletions propagate
sleep 5

# Delete all KSail-owned placement groups
echo "🗑️  Deleting placement groups..."
PG_IDS=$(hcloud placement-group list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>/dev/null || true)
if [[ -n "${PG_IDS}" ]]; then
  echo "Found placement groups to delete:"
  hcloud placement-group list -l "${LABEL_SELECTOR}"
  for PG_ID in ${PG_IDS}; do
    echo "  Deleting placement group ID: ${PG_ID}"
    hcloud placement-group delete "${PG_ID}" || echo "⚠️  Failed to delete placement group ${PG_ID}"
  done
else
  echo "  No placement groups found"
fi

# Delete all KSail-owned firewalls (with retry for detachment delays)
echo "🗑️  Deleting firewalls..."
for ATTEMPT in {1..5}; do
  FW_IDS=$(hcloud firewall list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>/dev/null || true)
  if [[ -z "${FW_IDS}" ]]; then
    echo "  No firewalls found"
    break
  fi
  
  if [[ ${ATTEMPT} -eq 1 ]]; then
    echo "Found firewalls to delete:"
    hcloud firewall list -l "${LABEL_SELECTOR}"
  fi
  
  for FW_ID in ${FW_IDS}; do
    echo "  Deleting firewall ID: ${FW_ID} (attempt ${ATTEMPT}/5)"
    if hcloud firewall delete "${FW_ID}"; then
      echo "  ✓ Deleted firewall ${FW_ID}"
    else
      echo "  ⚠️  Failed to delete firewall ${FW_ID}, may be still attached"
    fi
  done
  
  # Check if any firewalls remain
  REMAINING=$(hcloud firewall list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>/dev/null || true)
  if [[ -z "${REMAINING}" ]]; then
    break
  fi
  
  # Wait before retry
  if [[ ${ATTEMPT} -lt 5 ]]; then
    echo "  Waiting 2s before retry..."
    sleep 2
  fi
done

# Delete all KSail-owned networks
echo "🗑️  Deleting networks..."
NET_IDS=$(hcloud network list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>/dev/null || true)
if [[ -n "${NET_IDS}" ]]; then
  echo "Found networks to delete:"
  hcloud network list -l "${LABEL_SELECTOR}"
  for NET_ID in ${NET_IDS}; do
    echo "  Deleting network ID: ${NET_ID}"
    hcloud network delete "${NET_ID}" || echo "⚠️  Failed to delete network ${NET_ID}"
  done
else
  echo "  No networks found"
fi

echo "✅ Hetzner Cloud cleanup complete!"
