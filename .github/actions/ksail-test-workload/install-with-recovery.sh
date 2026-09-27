#!/usr/bin/env bash
# Install the test Helm chart through `ksail workload install`, with up to three attempts.
#
# An attempt can fail after Helm has recorded the release, for example when it times out waiting for
# the Deployment. The release name then stays reserved and every later attempt fails at once, so the
# test-owned release is removed before the next attempt. If that removal fails, the retries stop.
set -euo pipefail

release="ksail-install-test"
timeout="${TIMEOUT:?TIMEOUT is required}"
retry_delay="${RETRY_DELAY:-10}"
attempts=3

remove_test_release() {
	ksail workload delete deployment/"$release"-podinfo --ignore-not-found &&
		ksail workload delete service/"$release"-podinfo --ignore-not-found &&
		ksail workload delete secret -l owner=helm,name="$release" --ignore-not-found
}

for attempt in $(seq 1 "$attempts"); do
	if ksail workload install "$release" oci://ghcr.io/stefanprodan/charts/podinfo --version=6.7.1 --wait --timeout="$timeout"; then
		echo "✅ workload install succeeded"
		exit 0
	fi
	echo "⚠️ workload install attempt $attempt failed"
	if [ "$attempt" -lt "$attempts" ]; then
		if ! remove_test_release; then
			echo "❌ could not remove the partial $release release before retrying"
			exit 1
		fi
		echo "Removed the partial $release release; retrying in ${retry_delay}s…"
		sleep "$retry_delay"
	fi
done

echo "❌ workload install failed after $attempts attempts"
exit 1
