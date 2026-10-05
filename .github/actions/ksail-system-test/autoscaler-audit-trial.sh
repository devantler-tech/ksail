#!/usr/bin/env bash
# Real-provider acceptance for the unchanged-update inventory audit (#7327).
# One powered-off, run-labelled server supplies an orphaned autoscaler identity;
# it is never a static KSail node, and no worker bootstrap or snapshot is needed.
set -euo pipefail
umask 077

: "${HCLOUD_TOKEN:?HCLOUD_TOKEN is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${CLUSTER_NAME:?CLUSTER_NAME is required}"
: "${K8S_VERSION:?K8S_VERSION is required}"
[[ "$GITHUB_RUN_ID" =~ ^[0-9]+$ && "$CLUSTER_NAME" == "st-hetzner-dispatch-$GITHUB_RUN_ID" ]] || {
	echo 'ERROR: trial requires the current dispatch-owned cluster' >&2
	exit 1
}
[[ "$K8S_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || exit 1

probe_name="$CLUSTER_NAME-audit-removed"
selector="ksail.trial.run=$GITHUB_RUN_ID"
evidence="${EVIDENCE_DIR:-/tmp/ksail-system-test-logs/autoscaler-audit}"
mkdir -p "$evidence"
scratch=$(mktemp -d)
config_before=$(sha256sum ksail.yaml)
accepted=false

probe_ids() {
	timeout --kill-after=10s 1m hcloud server list -o noheader -o columns=id -l "$selector"
}

cleanup_probe() {
	local ids id
	ids=$(probe_ids) || return 1
	for id in $ids; do
		# Verify both the immutable run label and the exact name before deleting.
		timeout --kill-after=10s 1m hcloud server describe "$id" -o json |
			jq -e --arg name "$probe_name" --arg run "$GITHUB_RUN_ID" \
				'.name == $name and .labels["ksail.trial.run"] == $run' >/dev/null || return 1
		timeout --kill-after=10s 2m hcloud server delete "$id" >/dev/null || return 1
	done
	ids=$(probe_ids) || return 1
	[[ -z "$ids" ]] || {
		echo 'ERROR: probe still exists after cleanup' >&2
		return 1
	}
}

finish() {
	local status=$?
	trap - EXIT
	if ! cleanup_probe; then
		echo 'ERROR: trial-owned probe cleanup or absence verification failed' >&2
		status=1
	fi
	rm -rf "$scratch"
	if [[ "$status" -eq 0 && "$accepted" == true ]]; then
		printf '%s\n' '{"foreignNetworkExcluded":true,"unchangedFailures":2,"serverPreserved":true,"configPreserved":true,"probeAbsent":true}' >"$evidence/acceptance.json"
		echo 'PASS: foreign network excluded; both unchanged updates failed; server preserved; probe absent'
	fi
	exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Refuse reuse of an earlier invocation; cleanup still owns this exact run/name.
[[ -z "$(probe_ids)" ]] || {
	echo 'ERROR: trial probe already exists' >&2
	exit 1
}

# Filter the create response in the producing pipeline: it contains a root
# password which must never enter evidence or logs. No SSH key or snapshot is made.
probe_id=$(timeout --kill-after=10s 3m hcloud server create \
	--name "$probe_name" --type "${TRIAL_SERVER_TYPE:-cx23}" \
	--location "${TRIAL_LOCATION:-fsn1}" --image ubuntu-24.04 \
	--start-after-create=false --without-ipv4 \
	--label "ksail.cluster.name=$CLUSTER_NAME,ksail.trial.run=$GITHUB_RUN_ID,hcloud/node-group=removed-pool" \
	-o json | jq -er '.server.id')
[[ "$probe_id" =~ ^[0-9]+$ ]] || exit 1

update() {
	timeout --kill-after=10s 5m ksail cluster update --force \
		--distribution Talos --provider Hetzner --name "$CLUSTER_NAME" \
		--kubernetes-version "$K8S_VERSION" >"$scratch/update.log" 2>&1
}

# A server with the same pool label outside this network must be ignored.
update || {
	echo 'ERROR: foreign-network inventory prevented an unchanged update' >&2
	exit 1
}
grep -Fq 'No changes detected' "$scratch/update.log" || exit 1
! grep -Fq "$probe_name is left untouched" "$scratch/update.log" || exit 1

timeout --kill-after=10s 2m hcloud server attach-to-network "$probe_id" \
	--network "$CLUSTER_NAME-network" >/dev/null

fingerprint() {
	timeout --kill-after=10s 1m hcloud server describe "$probe_id" -o json |
		jq -cS '{id,name,created,status,serverType:.server_type.id,image:.image.id,labels,networks:[.private_net[]?.network]}' |
		sha256sum
}
server_before=$(fingerprint)

# The ordinary fixture disables autoscaling. This real orphan belongs to no
# configured pool: each fresh, zero-diff command must report it and fail.
for attempt in 1 2; do
	if update; then
		echo "ERROR: unchanged update $attempt silently accepted a leftover server" >&2
		exit 1
	fi
	grep -Fq "$probe_name is left untouched" "$scratch/update.log" || exit 1
	grep -Fq 'node autoscaler is disabled' "$scratch/update.log" || exit 1
	grep -Fq '1 changes failed to apply:' "$scratch/update.log" || exit 1
	! grep -Fq 'No changes detected' "$scratch/update.log" || exit 1
	[[ "$(fingerprint)" == "$server_before" ]] || {
		echo 'ERROR: audit changed the probe' >&2
		exit 1
	}
	[[ "$(sha256sum ksail.yaml)" == "$config_before" ]] || exit 1
done
accepted=true
