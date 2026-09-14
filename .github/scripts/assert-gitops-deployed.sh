#!/usr/bin/env bash
#
# Asserts that a GitOps reconcile actually deployed something, not merely that it ran.
#
# `ksail workload push` and `ksail workload reconcile` both exit 0 when the engine deploys
# nothing: Argo CD renders an empty source as zero resources and reports Synced/Healthy, and a
# Flux Kustomization with an empty inventory is Ready too. That is how the #6284 ArgoCD defect
# shipped with every GitOps system test green. This script checks the outcome instead: the root
# object manages at least one resource, the fixture ConfigMap the test pushed is among them, and
# that ConfigMap exists in the cluster.
#
# Usage: assert-gitops-deployed.sh <Flux|ArgoCD> <fixture-configmap-name>
#
#   <Flux|ArgoCD>              The GitOps engine the cluster was created with.
#   <fixture-configmap-name>   Name of the ConfigMap the test pushed in namespace `default`.
#
# Environment:
#   KSAIL                       ksail command to use (default: ksail).
#   ASSERT_GITOPS_ATTEMPTS      How many times to read before failing (default: 30).
#   ASSERT_GITOPS_INTERVAL      Seconds between reads (default: 10).
#
# Exit status: 0 when the fixture is deployed and managed by the root object; 1 otherwise, with a
# message naming what was expected and what the engine actually managed; 2 on invalid usage.

set -euo pipefail

# Print the required arguments to stderr and reject an invalid invocation with exit 2.
usage() {
	printf 'usage: %s <Flux|ArgoCD> <fixture-configmap-name>\n' "${0##*/}" >&2
	exit 2
}

[ "$#" -eq 2 ] || usage

engine="$1"
fixture="$2"
fixture_namespace="default"
ksail_cmd="${KSAIL:-ksail}"
attempts="${ASSERT_GITOPS_ATTEMPTS:-30}"
interval="${ASSERT_GITOPS_INTERVAL:-10}"

# Each engine exposes what it manages on its root object in a different shape; normalise both to
# one "Kind/namespace/name" line per managed resource.
case "${engine}" in
Flux)
	root_desc="Kustomization flux-system/flux-system"
	root_args=(workload get kustomization flux-system -n flux-system -o json)
	# Flux inventory ids are "<namespace>_<name>_<group>_<kind>".
	managed_filter='[.status.inventory.entries[]?.id | split("_") | "\(.[3])/\(.[0])/\(.[1])"] | .[]'
	;;
ArgoCD)
	root_desc="Application argocd/ksail"
	root_args=(workload get application ksail -n argocd -o json)
	managed_filter='[.status.resources[]? | "\(.kind)/\(.namespace // "")/\(.name)"] | .[]'
	;;
*)
	printf 'FAIL: unknown GitOps engine %q, expected Flux or ArgoCD\n' "${engine}" >&2
	exit 2
	;;
esac

expected="ConfigMap/${fixture_namespace}/${fixture}"
reason=""
managed=""

for attempt in $(seq 1 "${attempts}"); do
	reason=""
	if ! root_json="$("${ksail_cmd}" "${root_args[@]}" 2>&1)"; then
		reason="could not read ${root_desc}: ${root_json}"
	elif ! managed="$(jq -r "${managed_filter}" <<<"${root_json}" 2>&1)"; then
		reason="could not parse ${root_desc}: ${managed}"
		managed=""
	elif [ -z "${managed}" ]; then
		reason="${root_desc} manages 0 resources"
	elif ! grep -qxF -- "${expected}" <<<"${managed}"; then
		reason="${root_desc} does not manage ${expected}"
	elif ! "${ksail_cmd}" workload get configmap "${fixture}" -n "${fixture_namespace}" -o name >/dev/null 2>&1; then
		reason="${root_desc} lists ${expected}, but it does not exist in the cluster"
	else
		count="$(grep -c . <<<"${managed}")"
		printf 'OK: %s manages %d resource(s), including %s, which exists in the cluster\n' \
			"${root_desc}" "${count}" "${expected}"
		exit 0
	fi

	if [ "${attempt}" -lt "${attempts}" ]; then
		printf 'waiting (%d/%d): %s\n' "${attempt}" "${attempts}" "${reason}" >&2
		sleep "${interval}"
	fi
done

printf 'FAIL: %s\n' "${reason}" >&2
printf '  expected: %s\n' "${expected}" >&2
printf '  managed:  %s\n' "$(tr '\n' ' ' <<<"${managed:-<none>}")" >&2
exit 1
