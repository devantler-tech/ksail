#!/usr/bin/env bash
#
# Asserts who creates the k3k server pod, and records the evidence.
#
# The k3k privileged-pod guard (pkg/svc/provisioner/cluster/k3d/kubernetes_provisioner.go,
# serverPodExemptionTemplate) admits the privileged k3k server pod only when the request
# principal is kube-controller-manager's StatefulSet controller. The guard runs with
# FailurePolicy Fail, so if a k3k release ever changed who creates that pod, every k3k
# cluster would become unprovisionable. This script pins that premise against a live
# cluster: the server pod must be owned by a StatefulSet and managed by
# kube-controller-manager.
#
# Usage: assert-k3k-server-creator.sh <cluster-name> <evidence-dir>
#
#   <cluster-name>  The KSail cluster name. The k3k server pod lives in namespace
#                   k3k-<cluster-name> and carries the labels role=server,cluster=<cluster-name>.
#   <evidence-dir>  Directory that receives k3k-server-pod-creator.json — the pod's
#                   ownerReferences and managedFields managers — so the evidence survives the run.
#
# Environment:
#   KUBECTL                     kubectl command to use (default: kubectl).
#   KSAIL_K3K_SERVER_POD_JSON   Test seam: path to a file holding the `kubectl get pods -o json`
#                               list to evaluate instead of reading a live cluster.
#
# Exit status: 0 when every server pod satisfies both assertions; 1 otherwise, with a message
# naming what was found instead.

set -euo pipefail

usage() {
	printf 'usage: %s <cluster-name> <evidence-dir>\n' "${0##*/}" >&2
	exit 2
}

[ "$#" -eq 2 ] || usage

cluster="$1"
evidence_dir="$2"
namespace="k3k-${cluster}"
selector="role=server,cluster=${cluster}"
kubectl_cmd="${KUBECTL:-kubectl}"

expected_owner_kind="StatefulSet"
expected_manager="kube-controller-manager"

mkdir -p "${evidence_dir}"
evidence_file="${evidence_dir}/k3k-server-pod-creator.json"

if [ -n "${KSAIL_K3K_SERVER_POD_JSON:-}" ]; then
	pods_json="$(cat "${KSAIL_K3K_SERVER_POD_JSON}")"
else
	# kubectl strips managedFields from JSON by default; retain the creator evidence.
	pods_json="$("${kubectl_cmd}" get pods -n "${namespace}" -l "${selector}" -o json --show-managed-fields=true)"
fi

# Keep only what the assertion reads (and what a reader needs to re-derive it): the
# controller owner and the managers that wrote the pod. The fieldsV1 blobs are dropped
# so the evidence stays legible.
evidence_json="$(
	jq '{
		namespace: "'"${namespace}"'",
		selector: "'"${selector}"'",
		pods: [ .items[] | {
			name: .metadata.name,
			ownerReferences: (.metadata.ownerReferences // []),
			managedFields: [ (.metadata.managedFields // [])[]
				| {manager, operation, subresource: (.subresource // ""), time: (.time // "")} ]
		} ]
	}' <<<"${pods_json}"
)"

printf '%s\n' "${evidence_json}" >"${evidence_file}"

pod_count="$(jq '.pods | length' <<<"${evidence_json}")"
if [ "${pod_count}" -eq 0 ]; then
	printf 'FAIL: no pod with labels %s found in namespace %s — the guard'"'"'s label assumptions do not match this k3k release\n' \
		"${selector}" "${namespace}" >&2
	exit 1
fi

failures=0

while IFS= read -r pod_name; do
	pod_query='.pods[] | select(.name == "'"${pod_name}"'")'

	# The controller owner: exactly one ownerReference flagged controller=true, of the expected kind.
	controller_kinds="$(jq -r "${pod_query}"' | [.ownerReferences[] | select(.controller == true) | .kind] | join(",")' <<<"${evidence_json}")"
	if [ "${controller_kinds}" != "${expected_owner_kind}" ]; then
		printf 'FAIL: pod %s/%s is controlled by [%s], expected exactly one %s owner\n' \
			"${namespace}" "${pod_name}" "${controller_kinds:-none}" "${expected_owner_kind}" >&2
		failures=$((failures + 1))
	fi

	# The managing component: the managers that wrote the pod object itself (not a
	# subresource such as status, which the kubelet owns) must include the StatefulSet
	# controller's component identity.
	managers="$(jq -r "${pod_query}"' | [.managedFields[] | select(.operation == "Update" and .subresource == "") | .manager] | unique | join(",")' <<<"${evidence_json}")"
	if ! printf '%s\n' "${managers}" | tr ',' '\n' | grep -qx -- "${expected_manager}"; then
		printf 'FAIL: pod %s/%s was written by managers [%s], expected %s among them\n' \
			"${namespace}" "${pod_name}" "${managers:-none}" "${expected_manager}" >&2
		failures=$((failures + 1))
	fi
done < <(jq -r '.pods[].name' <<<"${evidence_json}")

if [ "${failures}" -gt 0 ]; then
	printf 'FAIL: %d assertion(s) failed for %d server pod(s); evidence at %s\n' \
		"${failures}" "${pod_count}" "${evidence_file}" >&2
	exit 1
fi

printf 'OK: %d k3k server pod(s) in %s owned by a %s and written by %s; evidence at %s\n' \
	"${pod_count}" "${namespace}" "${expected_owner_kind}" "${expected_manager}" "${evidence_file}"
