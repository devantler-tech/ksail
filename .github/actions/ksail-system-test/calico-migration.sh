#!/usr/bin/env bash
# The old CLI is used only by the creation step. This trial runs the candidate CLI.
set -euo pipefail

fail() {
  echo "Calico migration trial: $*" >&2
  exit 1
}

case "${1:-}" in
prepare)
  # Called for every leg, so ordinary fresh-create tests keep the original PATH.
  if [[ -z "${BASELINE_BINARY:-}" ]]; then
    printf 'path=%s\n' "$PATH" >>"$GITHUB_OUTPUT"
    exit 0
  fi
  [[ "${PROVIDER:-}" == Docker && "${INIT:-}" == true ]] || fail 'migration requires initialized Docker clusters'
  case "${DISTRIBUTION:-}" in Vanilla | K3s | VCluster) ;; *) fail 'unsupported migration distribution' ;; esac
  [[ "$BASELINE_BINARY" == /*/ksail && -f "$BASELINE_BINARY" && ! -L "$BASELINE_BINARY" && -s "$BASELINE_BINARY" ]] || fail 'missing or invalid baseline binary'
  [[ "${BASELINE_SHA256:-}" =~ ^[a-f0-9]{64}$ ]] || fail 'missing producer checksum'
  actual=$(shasum -a 256 "$BASELINE_BINARY" | cut -d ' ' -f 1)
  [[ "$actual" == "$BASELINE_SHA256" ]] || fail 'baseline checksum differs from its producer'
  chmod +x "$BASELINE_BINARY"
  printf 'path=%s:%s\n' "$(dirname "$BASELINE_BINARY")" "$PATH" >>"$GITHUB_OUTPUT"
  exit 0
  ;;
migrate) ;;
*) fail 'expected prepare or migrate' ;;
esac

log_dir="${SYSTEM_TEST_LOG_DIR:-/tmp/ksail-system-test-logs}/calico-migration"
mkdir -p "$log_dir"
config_file="$PWD/ksail.yaml"
[[ -f "$config_file" && ! -L "$config_file" ]] || fail 'created cluster configuration is missing'
[[ -n "${KUBECONFIG:-}" && "$KUBECONFIG" == "$RUNNER_TEMP/calico-migration.kubeconfig" && -f "$KUBECONFIG" && ! -L "$KUBECONFIG" ]] || fail 'private creation kubeconfig is missing'
[[ "$(yq -r '.spec.cluster.connection.kubeconfig' "$config_file")" == "$KUBECONFIG" ]] || fail 'desired configuration differs from the private creation kubeconfig'
kubectl --kubeconfig "$KUBECONFIG" config view --output json >"$log_dir/target.json"
target_context=$(jq -er '
  select((.contexts | length) == 1 and (.clusters | length) == 1) |
  .["current-context"] as $context |
  select($context == .contexts[0].name and .contexts[0].context.cluster == .clusters[0].name) |
  $context | select(type == "string" and length > 0)
' "$log_dir/target.json") || fail 'creation kubeconfig does not identify exactly one cluster'
cli_target=(--kubeconfig "$KUBECONFIG" --context "$target_context")
helm_target=(--kubeconfig "$KUBECONFIG" --kube-context "$target_context")

# Record exact Helm metadata and prerequisite UIDs before any candidate mutation.
helm list --all --namespace tigera-operator --output json "${helm_target[@]}" >"$log_dir/before-releases.json"
jq -e '
  map(select(.name == "calico" or .name == "calico-crds")) |
  (map(.name) | sort) == ["calico", "calico-crds"] and
  all(.[]; .status == "deployed" and (.chart | test("-v?3\\.32\\.2$")))
' "$log_dir/before-releases.json" >/dev/null || fail 'baseline did not create two deployed Calico 3.32.2 releases'
read_legacy_history() {
  # Print metadata fields only; Helm release payloads never enter diagnostics.
  kubectl get secrets,configmaps --namespace tigera-operator --selector owner=helm,name=calico-crds \
    --request-timeout=30s "${cli_target[@]}" \
    --output='go-template={{range .items}}{{.kind}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{.metadata.uid}}{{"\n"}}{{end}}' |
    jq -R -s -e 'split("\n") | map(select(length > 0) | split("\t")) |
      select(length > 0 and all(.[]; length == 3 and (.[0] == "Secret" or .[0] == "ConfigMap") and .[1] != "" and .[2] != "")) |
      map(
      {resource: (if .[0] == "Secret" then "secrets" else "configmaps" end), name: .[1], uid: .[2], namespace:"tigera-operator"}) |
      select(length > 0) | sort_by(.resource, .name)'
}
read_legacy_history >"$log_dir/before-history.json" || fail 'baseline Helm history metadata is missing or incomplete'
resource_types='customresourcedefinitions.apiextensions.k8s.io,mutatingadmissionpolicies.admissionregistration.k8s.io,mutatingadmissionpolicybindings.admissionregistration.k8s.io,validatingadmissionpolicies.admissionregistration.k8s.io,validatingadmissionpolicybindings.admissionregistration.k8s.io'
read_prerequisites() {
  kubectl get "$resource_types" --request-timeout=30s --output json "${cli_target[@]}" |
    jq -e '[.items[] | select(.metadata.annotations["meta.helm.sh/release-name"] == "calico-crds" and
      .metadata.annotations["meta.helm.sh/release-namespace"] == "tigera-operator") |
      {kind, name: .metadata.name, uid: .metadata.uid}] |
      select(length > 0 and all(.[]; (.uid | type == "string" and length > 0))) | sort_by(.kind, .name)'
}
read_prerequisites >"$log_dir/before-identities.json"
jq -e 'length == 39' "$log_dir/before-identities.json" >/dev/null || fail 'legacy prerequisite capture is incomplete'
cp "$config_file" "$log_dir/before-config.yaml"
kubectl version --request-timeout=30s --output json "${cli_target[@]}" >"$log_dir/before-version.json"
version=$(jq -er '.serverVersion.gitVersion | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+([-+][0-9A-Za-z.-]+)?$"))' "$log_dir/before-version.json")
version_args=()
# vCluster does not expose a Kubernetes version override. Its rendered default is
# unchanged at this immutable baseline; Vanilla/K3s pin the observed running image.
if [[ "$DISTRIBUTION" == K3s ]]; then
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\+k3s[0-9]+$ ]] || fail 'K3s server version is not a stable image baseline'
  version="${version/+k3s/-k3s}"
fi
if [[ "$DISTRIBUTION" != VCluster ]]; then version_args=(--kubernetes-version "$version"); fi

status=0
ksail cluster diff --config "$config_file" --output json --exit-code "${cli_target[@]}" \
  >"$log_dir/before-diff.json" 2>"$log_dir/before-diff.stderr" || status=$?
[[ "$status" == 2 ]] || fail 'candidate did not detect the legacy prerequisite installation'
jq -e -s 'length == 1 and (.[0] | .totalChanges == 1 and
  (.inPlaceChanges | length == 1) and .inPlaceChanges[0].field == "cluster.cni.calico.prerequisites" and
  .rebootRequired == [] and .recreateRequired == [] and .rollingRecreate == [] and
  .wipeRequired == [] and .unknownBaseline == [])' "$log_dir/before-diff.json" >/dev/null || fail 'migration diff is not exactly one in-place prerequisite change'

ksail cluster update --config "$config_file" --yes --output json "${version_args[@]}" "${cli_target[@]}" \
  >"$log_dir/update.json" 2>"$log_dir/update.stderr"
cmp "$config_file" "$log_dir/before-config.yaml" || fail 'update changed the desired configuration'
helm list --all --namespace tigera-operator --output json "${helm_target[@]}" >"$log_dir/after-releases.json"
expected=$(sed -nE 's/^FROM docker.io\/calico\/node:(v[0-9]+\.[0-9]+\.[0-9]+)@sha256:.*/\1/p' "$GITHUB_WORKSPACE/pkg/svc/installer/cni/calico/Dockerfile")
[[ "$expected" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'candidate Calico version is missing'
jq -e --arg expected "${expected#v}" 'map(select(.name == "calico")) |
  length == 1 and .[0].status == "deployed" and (.[0].chart | endswith("-" + $expected) or endswith("-v" + $expected))' \
  "$log_dir/after-releases.json" >/dev/null || fail 'candidate operator release did not converge'
jq -e 'map(select(.name == "calico-crds")) | length == 1 and .[0].status == "deployed" and
  (.[0].chart | test("-v?3\\.32\\.2$"))' "$log_dir/after-releases.json" >/dev/null || fail 'legacy CRD release metadata was not retained'
read_legacy_history >"$log_dir/after-history.json" || fail 'retained Helm history metadata is missing or incomplete'
cmp "$log_dir/before-history.json" "$log_dir/after-history.json" || fail 'legacy Helm history identities changed'
kubectl get configmap ksail-calico-prerequisites --namespace tigera-operator --output json --request-timeout=30s \
  "${cli_target[@]}" >"$log_dir/inventory.json"
jq -e --arg expected "$expected" '
  select(.metadata.labels["ksail.io/component"] == "calico-prerequisites") |
  (.data["inventory.json"] | fromjson) |
  .complete == true and (.version == $expected or .version == ($expected | ltrimstr("v"))) and
  (.resources | map(select(.storage != true)) | length == 39) and
  all(.resources[]; (.uid | type == "string" and length > 0))
' "$log_dir/inventory.json" >/dev/null || fail 'candidate prerequisite inventory is incomplete'
jq -e --slurpfile history "$log_dir/before-history.json" '
  (.data["inventory.json"] | fromjson | .resources | map(select(.storage == true))) as $storage |
  all($history[0][]; . as $old | any($storage[]; .resource == $old.resource and .name == $old.name and
    .namespace == $old.namespace and .uid == $old.uid))
' "$log_dir/inventory.json" >/dev/null || fail 'legacy Helm history is absent from the inventory'
read_prerequisites >"$log_dir/after-identities.json"
jq -e -n --slurpfile before "$log_dir/before-identities.json" --slurpfile after "$log_dir/after-identities.json" \
  'all($before[0][]; . as $old | any($after[0][]; .kind == $old.kind and .name == $old.name and .uid == $old.uid))' >/dev/null || fail 'migration replaced a legacy prerequisite identity'
kubectl rollout status deployment/tigera-operator --namespace tigera-operator --timeout=5m "${cli_target[@]}"
# The operator updates the DaemonSet asynchronously. Waiting only for its old
# ready revision could report success before the new node image was selected.
converged=false
for ((attempt = 0; attempt < 36; attempt++)); do
  kubectl get daemonset calico-node --namespace calico-system --request-timeout=30s --output json "${cli_target[@]}" >"$log_dir/node-image.json"
  if jq -e --arg expected "$expected" 'any(.spec.template.spec.containers[]; .name == "calico-node" and (.image | endswith("/node:" + $expected)))' "$log_dir/node-image.json" >/dev/null; then
    converged=true
    break
  fi
  sleep 10
done
[[ "$converged" == true ]] || fail 'Calico node image did not converge on the candidate version'
kubectl rollout status daemonset/calico-node --namespace calico-system --timeout=5m "${cli_target[@]}"
status=0
ksail cluster diff --config "$config_file" --output json --exit-code "${cli_target[@]}" \
  >"$log_dir/after-diff.json" 2>"$log_dir/after-diff.stderr" || status=$?
[[ "$status" == 0 ]] || fail "final diff exited $status; see after-diff.json and after-diff.stderr"
jq -e -s 'length == 1 and (.[0] | .totalChanges == 0 and .inPlaceChanges == [] and
  .rebootRequired == [] and .recreateRequired == [] and .rollingRecreate == [] and
  .wipeRequired == [] and .unknownBaseline == [])' "$log_dir/after-diff.json" >/dev/null || fail 'migration did not converge without configuration changes'
echo 'PASS: legacy Calico installation migrated with stable prerequisite UIDs and unchanged configuration'
