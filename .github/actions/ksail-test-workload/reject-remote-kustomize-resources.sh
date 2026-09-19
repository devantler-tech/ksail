#!/usr/bin/env bash
set -euo pipefail

overlay_path=${1:?usage: reject-remote-kustomize-resources.sh OVERLAY_PATH}
remote=""

while IFS= read -r -d '' kustomization; do
	if matches="$(yq -r '(.resources // [])[], (.bases // [])[], (.components // [])[]' \
		"$kustomization" | grep -Ein '^(git::)?(https?://|git@|ssh://|github\.com[:/])')"; then
		remote="${remote}${kustomization}: ${matches}"$'\n'
	fi
done < <(find "$overlay_path" -type f \
	\( -name kustomization.yaml -o -name kustomization.yml -o -name Kustomization \) -print0)

if [ -n "$remote" ]; then
	echo "❌ overlay lists a remote resource; keep the system-test overlay local:"
	echo "${remote%$'\n'}"
	exit 1
fi
