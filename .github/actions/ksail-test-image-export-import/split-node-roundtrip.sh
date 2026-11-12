#!/usr/bin/env bash
# Prove that `ksail workload export` gathers images held on different nodes of one cluster, and
# that each archive restores exactly that content on every node (#8251, #7825).
#
# Each node of the explicitly named two-node CI cluster receives a tiny image, built locally from
# content no other node holds under any name. An export that leaves out a worker-only image, adds
# an image nobody asked for, or restores different content therefore fails here; with shared
# content it could have passed. Every inventory read must succeed: an unreadable node fails the
# check rather than reading as "image absent".
#
# The orchestrator creates and deletes the cluster. This only adds the two fixture images and
# removes them again, so it needs no registry and no extra runner.
set -euo pipefail

[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]] || {
	echo 'The split-image fixture only runs on a GitHub-hosted CI runner.' >&2
	exit 1
}

cluster="${IMAGE_EXPORT_CLUSTER:?explicit fixture cluster required}"
[[ "$cluster" == nested-kind-routing ]] || {
	echo "Refusing an unexpected cluster identity: $cluster" >&2
	exit 1
}

nodes=("$cluster-control-plane" "$cluster-worker")
roles=(control-plane worker)
refs=(localhost/ksail-split-export-a:fixture localhost/ksail-split-export-b:fixture)
digests=()

for index in 0 1; do
	labels=$(docker inspect --format \
		'{{index .Config.Labels "io.x-k8s.kind.cluster"}} {{index .Config.Labels "io.x-k8s.kind.role"}}' \
		"${nodes[$index]}")
	[[ "$labels" == "$cluster ${roles[$index]}" ]] || {
		echo "${nodes[$index]} is not the ${roles[$index]} of $cluster." >&2
		exit 1
	}
done

# inventory prints "<reference> <digest>" for every image the node stores. A failed or
# unrecognised read returns non-zero, so callers must assign it before testing the result.
inventory() {
	local node="$1" listing

	if ! listing=$(docker exec "$node" ctr --namespace=k8s.io images list); then
		echo "Cannot read the image inventory of $node." >&2
		return 1
	fi

	if ! awk 'NR == 1 { if ($1 != "REF" || $3 != "DIGEST") exit 1; next } { print $1, $3 }' \
		<<<"$listing"; then
		echo "The image inventory of $node has an unexpected format." >&2
		return 1
	fi
}

# digest_on prints the digest the node stores under ref, or nothing when it has no such image.
digest_on() {
	local entries

	entries=$(inventory "$1") || return 1
	awk -v ref="$2" '$1 == ref { print $2; exit }' <<<"$entries"
}

# refs_holding prints every reference on the node that points at the given digest.
refs_holding() {
	local entries

	entries=$(inventory "$1") || return 1
	awk -v digest="$2" '$2 == digest { print $1 }' <<<"$entries"
}

# remove_fixture_refs removes every name for fixture content from every node, including any
# digest alias an import added, so the next import has to restore the images by itself.
remove_fixture_refs() {
	local node digest held

	for node in "${nodes[@]}"; do
		# Cleanup can run before any fixture exists; expand an empty list safely under set -u.
		for digest in ${digests[@]+"${digests[@]}"}; do
			held=$(refs_holding "$node" "$digest") || return 1
			[[ -n "$held" ]] || continue

			# shellcheck disable=SC2086 # One reference per line; references contain no spaces.
			docker exec "$node" ctr --namespace=k8s.io images rm $held >/dev/null || {
				echo "Cannot remove the fixture images from $node." >&2
				return 1
			}
		done
	done
}

staging=$(mktemp -d "${RUNNER_TEMP:?}/split-image-export.XXXXXX")

cleanup() {
	local result=$? node ref

	trap - EXIT

	# Remove the fixture names even from a node whose inventory cannot be read, then every
	# remaining alias for fixture content, which needs the inventory.
	for node in "${nodes[@]}"; do
		for ref in "${refs[@]}"; do
			docker exec "$node" ctr --namespace=k8s.io images rm "$ref" >/dev/null 2>&1 || true
		done
	done

	if ! remove_fixture_refs; then
		result=1
	fi

	docker image rm --force "${refs[@]}" >/dev/null 2>&1 || true

	if ! rm -rf -- "$staging"; then
		result=1
	fi

	exit "$result"
}
trap cleanup EXIT

for index in 0 1; do
	node="${nodes[$index]}"
	ref="${refs[$index]}"
	rootfs="$staging/rootfs-$index"

	mkdir "$rootfs"
	printf 'KSail split-image export fixture for the %s, run %s.%s\n' \
		"${roles[$index]}" "${GITHUB_RUN_ID:-local}" "${GITHUB_RUN_ATTEMPT:-1}" >"$rootfs/fixture"
	tar -C "$rootfs" -cf - fixture | docker image import - "$ref" >/dev/null
	docker image save "$ref" |
		docker exec -i "$node" ctr --namespace=k8s.io images import --all-platforms - >/dev/null

	digest=$(digest_on "$node" "$ref")
	[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
		echo "$node did not store $ref after the fixture import." >&2
		exit 1
	}
	digests[index]="$digest"
done

[[ "${digests[0]}" != "${digests[1]}" ]] || {
	echo 'The two fixture images must differ in content.' >&2
	exit 1
}

# Each fixture's content must start on exactly one node, under any name.
for index in 0 1; do
	other="${nodes[1 - index]}"
	held=$(refs_holding "$other" "${digests[$index]}")
	[[ -z "$held" ]] || {
		echo "$other already holds the content of ${refs[$index]}; the fixture is not split." >&2
		exit 1
	}
done

echo "Split the fixture: ${refs[0]} only on ${nodes[0]}, ${refs[1]} only on ${nodes[1]}."

# archive_names prints each image name the archive's index declares, sorted and de-duplicated.
archive_names() {
	local archive="$1" index

	# An archive may store its index as index.json or ./index.json.
	if ! index=$(tar -xOf "$archive" index.json 2>/dev/null || tar -xOf "$archive" ./index.json); then
		echo "${archive##*/} has no readable index.json." >&2
		return 1
	fi

	jq -er '.manifests | if type == "array" then .[] else error("manifests is not a list") end |
		.annotations["io.containerd.image.name"] // .annotations["org.opencontainers.image.ref.name"] //
		empty' <<<"$index" | LC_ALL=C sort -u
}

expected_selected=$(printf '%s\n' "${refs[@]}" | LC_ALL=C sort)

ksail workload export "$staging/selected.tar" --image="${refs[0]}" --image="${refs[1]}"
ksail workload export "$staging/all.tar"

for archive in selected all; do
	path="$staging/$archive.tar"
	test -s "$path"

	names=$(archive_names "$path")
	if [[ "$archive" == selected ]]; then
		[[ "$names" == "$expected_selected" ]] || {
			echo "The selected export must hold exactly the requested images; it holds:" >&2
			printf '%s\n' "$names" >&2
			exit 1
		}
	else
		for ref in "${refs[@]}"; do
			grep -Fxq -- "$ref" <<<"$names" || {
				echo "The full export left out $ref." >&2
				exit 1
			}
		done
	fi

	remove_fixture_refs
	ksail workload import "$path"

	for node in "${nodes[@]}"; do
		for index in 0 1; do
			restored=$(digest_on "$node" "${refs[$index]}")
			[[ "$restored" == "${digests[$index]}" ]] || {
				echo "Importing $archive.tar did not restore ${refs[$index]} on $node with its original content." >&2
				exit 1
			}
		done
	done

	rm -f -- "$path"
	echo "Restored both node-only images on both nodes from $archive.tar."
done
