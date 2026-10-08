#!/usr/bin/env bash
# Publish and verify the immutable chart manifest before release publication can succeed.
set -euo pipefail
export LC_ALL=C

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
bash "$root_dir/.github/scripts/validate-release-ref.sh"
version=${GITHUB_REF#refs/tags/v}
repository=ghcr.io/devantler-tech/charts/ksail-operator

helm package "$root_dir/charts/ksail-operator" --version "$version" --app-version "$version"
# Helm's receipt names the pushed artifact and its manifest digest. Do not sign a
# mutable tag or silently accept a missing, ambiguous or mismatched receipt.
if ! receipt=$(helm push "ksail-operator-${version}.tgz" oci://ghcr.io/devantler-tech/charts 2>&1); then
	printf '%s\n' "$receipt" >&2
	exit 1
fi
printf '%s\n' "$receipt"
pushed=$(sed -n 's/^Pushed: //p' <<<"$receipt")
digest=$(sed -n 's/^Digest: //p' <<<"$receipt")
if [[ $pushed != "$repository:$version" || ! $digest =~ ^sha256:[0-9a-f]{64}$ ]]; then
	printf 'Invalid chart push receipt: expected the release chart and one SHA-256 manifest digest.\n' >&2
	exit 1
fi

artifact="$repository@$digest"
cosign sign --yes "$artifact"
cosign verify \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	--certificate-identity "https://github.com/devantler-tech/ksail/.github/workflows/cd.yaml@$GITHUB_REF" \
	"$artifact"
