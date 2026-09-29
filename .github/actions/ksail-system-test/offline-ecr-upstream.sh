#!/usr/bin/env bash
# Give a Docker system-test leg a reachable registry with no ECR Public data.
# The same URL is used as the mirror proxy upstream and containerd fallback.
set -euo pipefail

case "${1:-}" in
start)
	: "${GITHUB_RUN_ID:?}"
	: "${GITHUB_RUN_ATTEMPT:?}"
	: "${GITHUB_OUTPUT:?}"
	gateway=$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')
	if [[ ! "$gateway" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		echo 'Docker bridge gateway is unavailable' >&2
		exit 1
	fi

	container="ksail-ecr-offline-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
	url="http://${gateway}:5505"
	docker run -d --name "$container" -p "${gateway}:5505:5000" registry:3 >/dev/null

	ready=false
	for _ in $(seq 1 30); do
		if curl -fsS "$url/v2/" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	if [ "$ready" != true ]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
		echo 'Empty ECR fallback registry did not become ready' >&2
		exit 1
	fi

	printf 'remote-url=%s\ncontainer-name=%s\n' "$url" "$container" >>"$GITHUB_OUTPUT"
	;;
stop)
	container=${2:-}
	if [[ ! "$container" =~ ^ksail-ecr-offline-[0-9]+-[0-9]+$ ]]; then
		echo 'Refusing to remove an unrecognized offline ECR container' >&2
		exit 1
	fi
	docker rm -f "$container" >/dev/null
	;;
*)
	echo 'usage: offline-ecr-upstream.sh start | stop CONTAINER_NAME' >&2
	exit 2
	;;
esac
