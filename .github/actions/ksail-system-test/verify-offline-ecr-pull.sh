#!/usr/bin/env bash
# Verify that a real VCluster consumer became ready with no ECR Public fallback.
set -euo pipefail

upstream=${1:-}
if [[ ! "$upstream" =~ ^http:/{2}[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+$ ]]; then
	echo 'Expected a local HTTP registry URL for the offline ECR check' >&2
	exit 1
fi

nodes=$(docker ps --format '{{.Names}}' --filter 'name=vcluster.cp.')
node=${nodes%%$'\n'*}
if [ -z "$node" ]; then
	echo 'VCluster control-plane node was not found' >&2
	exit 1
fi

if ! docker exec "$node" grep -Fxq "server = \"$upstream\"" \
	/etc/containerd/certs.d/ecr-public.aws.com/hosts.toml; then
	echo 'VCluster containerd still has a remote ECR fallback' >&2
	exit 1
fi

effective=$(docker exec "$node" containerd config dump)
if ! awk '
  /^[[:space:]]*\[/ {
    registry = ($0 ~ /io[.]containerd[.]cri[.]v1[.]images.*[.]registry\]$/ ||
                $0 ~ /io[.]containerd[.]grpc[.]v1[.]cri.*[.]registry\]$/)
  }
  registry && /^[[:space:]]*config_path[[:space:]]*=/ {
    value = $0
    sub(/^[^=]*=[[:space:]]*/, "", value)
    gsub(/["\047]/, "", value)
    count = split(value, paths, ":")
    for (i = 1; i <= count; i++) {
      if (paths[i] == "/etc/containerd/certs.d") found = 1
    }
  }
  END { if (!found) exit 1 }
' <<<"$effective"; then
	echo 'VCluster CRI does not use the mirror hosts directory' >&2
	exit 1
fi

if ! cri_info=$(docker exec "$node" crictl info -o json 2>/dev/null); then
	echo 'VCluster CRI info command failed' >&2
	exit 1
fi
# Containerd 2.x does not expose the image registry config in CRI runtime info.
# Verify runtime readiness here; effective routing and the consumer pull are
# checked separately above and below.
if ! jq -e '(.status.conditions // []) | any(.type == "RuntimeReady" and .status == true)' \
	<<<"$cri_info" >/dev/null; then
	echo 'VCluster CRI runtime is not ready' >&2
	exit 1
fi

ready=$(ksail workload get deployment argocd-redis -n argocd \
	-o jsonpath='{.status.readyReplicas}')
if [[ ! "$ready" =~ ^[1-9][0-9]*$ ]]; then
	echo 'Argo CD Redis did not become ready from the validated mirror' >&2
	exit 1
fi

mirror="${node#vcluster.cp.}-ecr-public.aws.com"
mirror_log=$(docker logs "$mirror" 2>&1) || {
	echo 'VCluster ECR mirror logs are unavailable' >&2
	exit 1
}
if ! awk '
  /http[.]request[.]method=GET.*http[.]request[.]uri="?\/v2\/[^[:space:]"]*redis\/(manifests|blobs)\// &&
  /http[.]response[.]status=200/ { found = 1; exit }
  END { exit !found }
' <<<"$mirror_log"; then
	echo 'No successful Redis request was observed at the restored ECR mirror' >&2
	exit 1
fi

echo 'Argo CD Redis is ready through the restored ECR mirror with local-only fallback'
