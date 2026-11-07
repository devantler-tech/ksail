#!/usr/bin/env bash
# Exercise the candidate CLI against an existing, explicitly named CI-owned Kind
# cluster. The orchestrator creates and deletes it; this adds no runner or cluster.
set -euo pipefail

[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]] || {
	echo 'The split-image fixture only runs on a GitHub-hosted CI runner.' >&2
	exit 1
}
cluster="${IMAGE_EXPORT_CLUSTER:?explicit fixture cluster required}"
[[ "$cluster" == image-export-union ]] || {
	echo 'Refusing an unexpected cluster identity.' >&2
	exit 1
}
nodes=("$cluster-control-plane" "$cluster-worker")
for index in 0 1; do
	node="${nodes[$index]}"
	actual=$(docker inspect --format '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "$node")
	role=$(docker inspect --format '{{index .Config.Labels "io.x-k8s.kind.role"}}' "$node")
	expected=control-plane
	[[ "$index" == 0 ]] || expected=worker
	[[ "$actual" == "$cluster" && "$role" == "$expected" ]] || {
		echo 'Fixture node ownership does not match the explicit cluster and role.' >&2
		exit 1
	}
done

case "${1:-roundtrip}" in
assert-import-metadata)
	bash "$(dirname "${BASH_SOURCE[0]}")/assert-import-metadata.sh" "${nodes[@]}"
	exit
	;;
roundtrip) ;;
*)
	echo 'Unknown split-image fixture operation.' >&2
	exit 1
	;;
esac

ref_a="localhost/ksail-export-a:fixture"
ref_b="localhost/ksail-export-b:fixture"
staging=$(mktemp -d "${RUNNER_TEMP:?}/image-export-union.XXXXXX")
remove_fixture_refs() {
	local result=0
	for node in "${nodes[@]}"; do
		if ! docker exec "$node" ctr --namespace=k8s.io images rm "$ref_a" "$ref_b"; then
			result=1
		fi
	done
	return "$result"
}
cleanup() {
	result=$?
	trap - EXIT
	if ! remove_fixture_refs; then result=1; fi
	if ! rm -rf -- "$staging"; then result=1; fi
	exit "$result"
}
trap cleanup EXIT

# The preceding ordinary round trip imported the workload image into every
# node. Reuse it without another registry pull; only the aliases are split.
base="${WORKLOAD_IMAGE:?workload image required}"
first="${base%%/*}"
if [[ "$base" != */* ]]; then
	base="docker.io/library/$base"
elif [[ "$first" != *.* && "$first" != *:* && "$first" != localhost ]]; then
	base="docker.io/$base"
fi
for index in 0 1; do
	node="${nodes[$index]}"
	inventory=$(docker exec "$node" ctr --namespace=k8s.io images list -q)
	source=$(awk -v ref="$base" '$0 == ref || index($0, ref "@") == 1 {print; exit}' <<<"$inventory")
	[[ -n "$source" ]] || {
		echo 'Fixture workload image is absent from a node.' >&2
		exit 1
	}
	ref="$ref_a"
	[[ "$index" == 0 ]] || ref="$ref_b"
	docker exec "$node" ctr --namespace=k8s.io images tag "$source" "$ref"
done

has_ref() {
	local node="$1" ref="$2" inventory
	inventory=$(docker exec "$node" ctr --namespace=k8s.io images list -q) || return 1
	grep -Fxq -- "$ref" <<<"$inventory"
}
has_ref "${nodes[0]}" "$ref_a"
has_ref "${nodes[1]}" "$ref_b"
if has_ref "${nodes[0]}" "$ref_b" || has_ref "${nodes[1]}" "$ref_a"; then
	echo 'The fixture is not split across the two nodes.' >&2
	exit 1
fi

ksail workload export "$staging/selected.tar" --image="$ref_a" --image="$ref_b"
ksail workload export "$staging/all.tar"
for archive in "$staging/selected.tar" "$staging/all.tar"; do
	test -s "$archive"
	remove_fixture_refs
	ksail workload import "$archive"
	for node in "${nodes[@]}"; do
		has_ref "$node" "$ref_a"
		has_ref "$node" "$ref_b"
	done
	echo "Validated both split references after importing ${archive##*/} into both nodes."
done
