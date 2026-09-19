#!/usr/bin/env bash
set -euo pipefail

overlay_path=${1:?usage: reject-remote-kustomize-resources.sh OVERLAY_PATH}
remote_pattern='^([^=[:space:]]+=)?(git::)?(https?://|ssh://|[^/@[:space:]]+@[^/:[:space:]]+:|github\.com[:/])'
remote=""
visited=""

remote_capable_paths() {
	yq -r '[
		.resources[]?, .bases[]?, .components[]?, .crds[]?,
		.patches[]?.path?, .patchesJson6902[]?.path?, .replacements[]?.path?,
		.configurations[]?, .generators[]?, .transformers[]?, .validators[]?,
		.configMapGenerator[]?.files[]?, .configMapGenerator[]?.envs[]?,
		.configMapGenerator[]?.env?, .secretGenerator[]?.files[]?,
		.secretGenerator[]?.envs[]?, .secretGenerator[]?.env?,
		.openapi.path?, .helmCharts[]?.repo?, .helmCharts[]?.valuesFile?,
		.helmCharts[]?.additionalValuesFiles[]?,
		.helmChartInflationGenerator[]?.chartRepoUrl?,
		.helmChartInflationGenerator[]?.values?
	] | .[] | select(. != null and . != "")' "$1"
}

local_kustomization_refs() {
	yq -r '(.resources // [])[], (.bases // [])[], (.components // [])[]' "$1"
}

scan_kustomization() {
	local kustomization=$1 canonical matches ref candidate name
	canonical=$(realpath "$kustomization")
	if printf '%s' "$visited" | grep -Fqx "$canonical"; then
		return
	fi
	visited="${visited}${canonical}"$'\n'

	if matches="$(remote_capable_paths "$canonical" | grep -Ein "$remote_pattern")"; then
		remote="${remote}${canonical}: ${matches}"$'\n'
	fi

	while IFS= read -r ref; do
		if [ -z "$ref" ] || printf '%s\n' "$ref" | grep -Eiq "$remote_pattern"; then
			continue
		fi
		candidate="$(dirname "$canonical")/$ref"
		if [ -d "$candidate" ]; then
			for name in kustomization.yaml kustomization.yml Kustomization; do
				if [ -f "$candidate/$name" ]; then
					scan_kustomization "$candidate/$name"
					break
				fi
			done
		elif [ -f "$candidate" ]; then
			case "$(basename "$candidate")" in
				kustomization.yaml | kustomization.yml | Kustomization)
					scan_kustomization "$candidate"
					;;
			esac
		fi
	done < <(local_kustomization_refs "$canonical")
}

if [ -d "$overlay_path" ]; then
	for name in kustomization.yaml kustomization.yml Kustomization; do
		if [ -f "$overlay_path/$name" ]; then
			scan_kustomization "$overlay_path/$name"
			break
		fi
	done
else
	scan_kustomization "$overlay_path"
fi

if [ -n "$remote" ]; then
	echo "❌ overlay lists a remote resource; keep the system-test overlay local:"
	echo "${remote%$'\n'}"
	exit 1
fi
