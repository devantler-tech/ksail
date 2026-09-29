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

ready=$(ksail workload get deployment argocd-redis -n argocd \
  -o jsonpath='{.status.readyReplicas}')
if [[ ! "$ready" =~ ^[1-9][0-9]*$ ]]; then
  echo 'Argo CD Redis did not become ready from the validated mirror' >&2
  exit 1
fi

echo 'Argo CD Redis is ready with local-only ECR fallback'
