#!/usr/bin/env bash
# cleanup-hetzner.sh — delete every Hetzner Cloud resource matching LABEL_SELECTOR (#7435).
#
# Each resource kind is cleaned even when an earlier kind failed, so one bad call never strands
# the rest. A failed list call is a failure, never "nothing to delete": the script finishes the
# remaining kinds, then exits 1 so the leak is visible on the run.

set -uo pipefail

: "${LABEL_SELECTOR:?LABEL_SELECTOR must be set}"

failed=0

# list_ids KIND — print the IDs of KIND resources matching LABEL_SELECTOR. It runs inside a command
# substitution, so a failed list call reports on stderr and returns 1; the caller records the failure.
list_ids() {
	local kind="$1" ids err_file
	err_file=$(mktemp)
	if ! ids=$(hcloud "${kind}" list -o noheader -o columns=id -l "${LABEL_SELECTOR}" 2>"${err_file}"); then
		echo "❌ Failed to list ${kind} resources: $(<"${err_file}")" >&2
		rm -f "${err_file}"
		return 1
	fi
	rm -f "${err_file}"
	printf '%s' "${ids}"
}

# delete_kind KIND LABEL — delete every KIND resource matching LABEL_SELECTOR once.
delete_kind() {
	local kind="$1" label="$2" ids id
	echo "🗑️  Deleting ${label}..."
	ids=$(list_ids "${kind}") || {
		failed=1
		return 0
	}
	if [[ -z "${ids}" ]]; then
		echo "  No ${label} found"
		return 0
	fi
	for id in ${ids}; do
		echo "  Deleting ${kind} ID: ${id}"
		if ! hcloud "${kind}" delete "${id}"; then
			echo "❌ Failed to delete ${kind} ${id}"
			failed=1
		fi
	done
}

echo "🧹 Starting Hetzner Cloud cleanup for KSail-owned resources..."

# Validate token access using the environment token. Probe Locations:
# Hetzner removed the Datacenters endpoints on 2026-10-01 (HTTP 410).
if ! probe_error=$(hcloud location list -o noheader 2>&1 >/dev/null); then
	echo "❌ Failed to access Hetzner Cloud: ${probe_error}"
	exit 1
fi

echo "🔍 Finding KSail-owned resources with label: ${LABEL_SELECTOR}"

delete_kind server servers

# Small delay to let server deletions propagate
sleep 5

# A floating IP still assigned to a server cannot be deleted, so unassign it and retry once.
echo "🗑️  Deleting floating IPs..."
if ! FIP_IDS=$(list_ids floating-ip); then
	failed=1
else
	if [[ -z "${FIP_IDS}" ]]; then
		echo "  No floating IPs found"
	fi
	for FIP_ID in ${FIP_IDS}; do
		echo "  Deleting floating-ip ID: ${FIP_ID}"
		if ! hcloud floating-ip delete "${FIP_ID}"; then
			echo "  Unassigning floating-ip ${FIP_ID} before retrying"
			if ! { hcloud floating-ip unassign "${FIP_ID}" && hcloud floating-ip delete "${FIP_ID}"; }; then
				echo "❌ Failed to delete floating-ip ${FIP_ID}"
				failed=1
			fi
		fi
	done
fi

delete_kind placement-group "placement groups"

# Delete all KSail-owned firewalls (with retry for detachment delays)
echo "🗑️  Deleting firewalls..."
for ATTEMPT in {1..5}; do
	FW_IDS=$(list_ids firewall) || {
		failed=1
		break
	}
	if [[ -z "${FW_IDS}" ]]; then
		[[ ${ATTEMPT} -eq 1 ]] && echo "  No firewalls found"
		break
	fi

	if [[ ${ATTEMPT} -eq 5 ]]; then
		echo "❌ Firewalls still present after 4 attempts: ${FW_IDS//$'\n'/ }"
		failed=1
		break
	fi

	for FW_ID in ${FW_IDS}; do
		echo "  Deleting firewall ID: ${FW_ID} (attempt ${ATTEMPT}/4)"
		if hcloud firewall delete "${FW_ID}"; then
			echo "  ✓ Deleted firewall ${FW_ID}"
		else
			echo "  ⚠️  Failed to delete firewall ${FW_ID}, may be still attached"
		fi
	done

	echo "  Waiting 2s before re-checking..."
	sleep 2
done

delete_kind network networks

if [[ ${failed} -ne 0 ]]; then
	echo "❌ Hetzner Cloud cleanup incomplete for ${LABEL_SELECTOR}: see the errors above"
	exit 1
fi

echo "✅ Hetzner Cloud cleanup complete!"
