#!/usr/bin/env bash

set -euo pipefail

candidate=beed8b6106333b370400874eb59226b57e06eeaa
[[ "${GITHUB_ACTIONS:-}" == true && "$(git rev-parse HEAD^)" == "$candidate" ]]
git diff --exit-code "$candidate" HEAD -- . \
	':(exclude).github/workflows/ci.yaml' \
	':(exclude).github/scripts/eval-split-image-export.sh'
printf 'Product source %s\n' "$candidate"
sha256sum "$(command -v ksail)"

trial=$(mktemp -d "$RUNNER_TEMP/ksail-image-export.XXXXXX")
cluster=ksail-image-eval
export KUBECONFIG="$trial/kubeconfig"
cd "$trial"

cleanup() {
	local status=$?
	local remaining
	trap - EXIT
	if [[ -f ksail.yaml ]]; then
		timeout --kill-after=10s 2m ksail cluster delete --provider Docker --name "$cluster" --force || status=1
	fi
	if ! remaining=$(docker ps -aq --filter "label=io.x-k8s.kind.cluster=$cluster") || [[ -n "$remaining" ]]; then
		echo 'Could not verify teardown of the trial Docker nodes' >&2
		status=1
	fi
	exit "$status"
}
trap cleanup EXIT

ksail project init --name "$cluster" --distribution Vanilla --provider Docker --workers 1 \
	--csi Disabled --load-balancer Disabled --metrics-server Disabled \
	--policy-engine None --gitops-engine None
ksail cluster create

ksail workload get nodes -o json | jq -r '.items[].metadata.name' | sort >nodes.txt
mapfile -t nodes <nodes.txt
[[ "${#nodes[@]}" == 2 ]]
[[ "${nodes[0]}" == "$cluster-control-plane" && "${nodes[1]}" == "$cluster-worker" ]]

first=docker.io/library/ksail-export-fixture-a:v1
second=docker.io/library/ksail-export-fixture-b:v1
image_digest() {
	docker exec "$1" ctr -n k8s.io images ls | awk -v ref="$2" '$1 == ref { print $3 }'
}

seed_image() {
	local node=$1 ref=$2 fixture=$3
	mkdir -p "$fixture"
	printf '%s\n' 'FROM scratch' 'COPY payload /payload' >"$fixture/Dockerfile"
	printf 'Distinct image payload %s\n' "$fixture" >"$fixture/payload"
	docker build --network=none --tag "$ref" "$fixture"
	docker save "$ref" | docker exec -i "$node" ctr -n k8s.io images import -
}

seed_image "${nodes[0]}" "$first" fixture-a
seed_image "${nodes[1]}" "$second" fixture-b
first_digest=$(image_digest "${nodes[0]}" "$first")
second_digest=$(image_digest "${nodes[1]}" "$second")
first_on_worker=$(image_digest "${nodes[1]}" "$first")
second_on_control_plane=$(image_digest "${nodes[0]}" "$second")
[[ "$first_digest" =~ ^sha256:[a-f0-9]{64}$ && "$second_digest" =~ ^sha256:[a-f0-9]{64}$ ]]
[[ "$first_digest" != "$second_digest" ]]
[[ -z "$first_on_worker" && -z "$second_on_control_plane" ]]
printf '%s\t%s\n' "$first" "$first_digest" "$second" "$second_digest" | sort >expected.tsv

archive_refs() {
	tar -xOf "$1" index.json | jq -r '
    .manifests[]
    | [(.annotations["io.containerd.image.name"] // .annotations["org.opencontainers.image.ref.name"] // ""), .digest]
    | select(.[0] != "")
    | @tsv' | sort
}

ksail workload export selected.tar --image="$first" --image="$second"
archive_refs selected.tar >selected.tsv
diff -u expected.tsv selected.tsv
ksail workload export all.tar
archive_refs all.tar | awk -v first="$first" -v second="$second" '$1 == first || $1 == second' >union-fixtures.tsv
diff -u expected.tsv union-fixtures.tsv

missing=docker.io/library/ksail-export-fixture-missing:v1
if ksail workload export missing.tar --image="$missing" >missing.log 2>&1; then
	echo 'Export unexpectedly succeeded for an image absent from both nodes' >&2
	exit 1
fi
grep -F "$missing" missing.log
[[ ! -e missing.tar ]]

for archive in selected.tar all.tar; do
	for node in "${nodes[@]}"; do
		for ref in "$first" "$second"; do
			digest=$(image_digest "$node" "$ref")
			if [[ -n "$digest" ]]; then
				docker exec "$node" ctr -n k8s.io images rm "$ref"
			fi
			digest=$(image_digest "$node" "$ref")
			[[ -z "$digest" ]]
		done
	done

	ksail workload import "$archive"
	for node in "${nodes[@]}"; do
		imported_first=$(image_digest "$node" "$first")
		imported_second=$(image_digest "$node" "$second")
		[[ "$imported_first" == "$first_digest" && "$imported_second" == "$second_digest" ]]
	done
	printf 'Verified %s imported both original image digests on both nodes\n' "$archive"
done
