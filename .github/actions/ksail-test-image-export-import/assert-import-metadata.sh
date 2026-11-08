#!/usr/bin/env bash
# Called only after the split-node fixture validated the CI-owned node labels.
# Inspect archive/reference joins without changing any stored image reference.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted &&
	"${IMAGE_EXPORT_CLUSTER:-}" == nested-kind-routing && "$#" == 2 &&
	"$1" == nested-kind-routing-control-plane && "$2" == nested-kind-routing-worker ]] || {
	echo 'Expected both validated fixture nodes on the GitHub-hosted runner.' >&2
	exit 1
}
staging=$(mktemp -d "${RUNNER_TEMP:?}/image-import-metadata.XXXXXX")
cleanup() {
	result=$?
	trap - EXIT
	if ! rm -rf -- "$staging"; then result=1; fi
	exit "$result"
}
trap cleanup EXIT
tar -tf /tmp/test-images.tar >"$staging/members"
[[ "$(grep -Fxc index.json "$staging/members")" == 1 ]] || {
	echo 'Expected exactly one archive index.' >&2
	exit 1
}
tar -xOf /tmp/test-images.tar index.json >"$staging/index.json"
jq -e '.schemaVersion == 2 and (.manifests | type) == "array" and (.manifests | length) > 0' "$staging/index.json" >/dev/null
root_record=$(sha256sum "$staging/index.json")
root_digest="sha256:${root_record%% *}"
printf 'Imported archive index digest: %s\n' "$root_digest"
jq -c '.manifests[]' "$staging/index.json" >"$staging/descriptors"
for node in "$@"; do
	docker exec "$node" ctr --namespace=k8s.io images list >"$staging/inventory"
	if ! awk -v root="$root_digest" '
    NR == 1 { valid = ($1 == "REF" && $3 == "DIGEST"); next }
    $3 == root { found = 1 }
    END { exit (!valid || found) }
  ' "$staging/inventory"; then
		echo 'Import stored the archive root as an image, or its inventory is unreadable.' >&2
		exit 1
	fi
	while IFS= read -r descriptor; do
		child_digest=$(jq -er '.digest | select(test("^sha256:[a-f0-9]{64}$"))' <<<"$descriptor")
		child_ref=$(jq -er '.annotations["io.containerd.image.name"] // .annotations["org.opencontainers.image.ref.name"] | select(type == "string" and length > 0)' <<<"$descriptor")
		[[ "$child_ref" =~ ^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$ ]] || {
			echo 'Archive child has an invalid image reference.' >&2
			exit 1
		}
		if ! awk -v name="$child_ref" -v target="$child_digest" '
      NR > 1 && $3 == target {
        if ($1 == name) named = 1
        suffix = "@" target
        if (index($1, "import-") == 1 && substr($1, length($1) - length(suffix) + 1) == suffix) digest = 1
      }
      END { exit (!named || !digest) }
    ' "$staging/inventory"; then
			echo 'An imported named image or child digest reference is missing or targets different content.' >&2
			exit 1
		fi
	done <"$staging/descriptors"
done
echo 'Validated archive-root absence and named/child-digest targets on both fixture nodes.'
