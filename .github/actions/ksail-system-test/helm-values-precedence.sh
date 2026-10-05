#!/usr/bin/env bash
# Reconcile the bundled chart with the existing job's native Flux installation.
set -euo pipefail

fail() {
	echo "Helm values precedence trial: $*" >&2
	exit 1
}

verify_observation() {
	# Require one complete document from each reader, fresh reconciliation of the
	# exact created release, Helm ownership, and equal values including JSON types.
	jq -e -n --slurpfile expected "$1" --slurpfile release "$2" --slurpfile child "$3" '
    ($expected | length) == 1 and ($release | length) == 1 and ($child | length) == 1 and
    ($expected[0] as $want | $release[0] as $actual | $child[0] as $rendered |
      ($want.values | type) == "object" and
      ($want.namespace | type) == "string" and ($want.namespace | length) > 0 and
      ($want.release | type) == "string" and ($want.release | length) > 0 and
      ($want.uid | type) == "string" and ($want.uid | length) > 0 and
      ($want.generation | type) == "number" and $want.generation > 0 and
      $actual.metadata.name == $want.release and
      $actual.metadata.namespace == $want.namespace and $actual.metadata.uid == $want.uid and
      $actual.metadata.generation == $want.generation and
      $actual.status.observedGeneration == $want.generation and
      ($actual.status.conditions | type) == "array" and
      ([ $actual.status.conditions[] | select(.type == "Ready") ] |
        length == 1 and .[0].status == "True" and .[0].observedGeneration == $want.generation) and
      all($actual.status.conditions[];
        ((.type != "Stalled" and .type != "Reconciling") or .status != "True")) and
      $rendered.metadata.name == "values-probe" and
      $rendered.metadata.namespace == $want.namespace and
      $rendered.metadata.annotations["meta.helm.sh/release-name"] == $want.release and
      $rendered.metadata.annotations["meta.helm.sh/release-namespace"] == $want.namespace and
      ($rendered.data["values.json"] | fromjson) == $want.values)
  ' >/dev/null || fail 'native Flux observation is incomplete or differs'
}

if [[ "${1:-}" == --verify-observation ]]; then
	[[ $# == 4 ]] || fail 'expected observation, release, and child files'
	verify_observation "$2" "$3" "$4"
	exit 0
fi
[[ $# == 0 ]] || fail 'unexpected trial arguments'
[[ "${DISTRIBUTION:-}" == Vanilla && "${PROVIDER:-}" == Docker && "${INIT:-}" == true ]] ||
	fail 'only the existing Docker/Vanilla initialized job is permitted'

for executable in kubectl helm jq docker; do
	command -v "$executable" >/dev/null || fail "$executable is required"
done
log_dir="${SYSTEM_TEST_LOG_DIR:-/tmp/ksail-system-test-logs}/helm-values-precedence"
mkdir -p "$log_dir"

# A named creation-time target plus its local Docker API port must agree. This
# prevents ambient kubeconfig or a reused context name selecting another cluster.
read -r -a create_args <<<"${ARGS:-}"
cluster_name=""
kubeconfig_file="$HOME/.kube/config"
name_count=0
kubeconfig_count=0
flux_count=0
for ((index = 0; index < ${#create_args[@]}; index++)); do
	case "${create_args[index]}" in
	--name | --kubeconfig | --gitops-engine)
		flag="${create_args[index]}"
		index=$((index + 1))
		[[ $index -lt ${#create_args[@]} ]] || fail 'creation-time target flag is missing its value'
		case "$flag" in
		--name)
			cluster_name="${create_args[index]}"
			name_count=$((name_count + 1))
			;;
		--kubeconfig)
			kubeconfig_file="${create_args[index]}"
			kubeconfig_count=$((kubeconfig_count + 1))
			;;
		--gitops-engine)
			[[ "${create_args[index]}" == Flux ]] || fail 'native Flux is required'
			flux_count=$((flux_count + 1))
			;;
		esac
		;;
	--name=*)
		cluster_name="${create_args[index]#--name=}"
		name_count=$((name_count + 1))
		;;
	--kubeconfig=*)
		kubeconfig_file="${create_args[index]#--kubeconfig=}"
		kubeconfig_count=$((kubeconfig_count + 1))
		;;
	--gitops-engine=*)
		[[ "${create_args[index]#--gitops-engine=}" == Flux ]] || fail 'native Flux is required'
		flux_count=$((flux_count + 1))
		;;
	esac
done
[[ $name_count == 1 && $flux_count == 1 && $kubeconfig_count -le 1 &&
	"$cluster_name" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail 'unambiguous explicit creation-time target is required'
if [[ "$kubeconfig_file" == \~/* ]]; then
	kubeconfig_file="$HOME/${kubeconfig_file#\~/}"
fi
[[ -f "$kubeconfig_file" ]] || fail 'creation-time kubeconfig file is missing'
target_context="kind-$cluster_name"
target=(--kubeconfig "$kubeconfig_file" --context "$target_context" --request-timeout=30s)
if [[ -n "${DOCKER_CONTEXT:-}" || -z "${DOCKER_HOST:-}" ]]; then
	docker_endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
else
	docker_endpoint="$DOCKER_HOST"
fi
[[ "$docker_endpoint" == unix:///* ]] || fail 'only the job-local Docker socket is permitted'
kubectl config view --minify --output json "${target[@]}" >"$log_dir/target.json"
docker inspect "${cluster_name}-control-plane" >"$log_dir/kind-container.json"
jq -e -n --arg target "$target_context" --arg cluster "$cluster_name" \
	--slurpfile config "$log_dir/target.json" --slurpfile containers "$log_dir/kind-container.json" '
  ($config | length) == 1 and ($containers | length) == 1 and ($containers[0] | length) == 1 and
  ($config[0] as $config | $containers[0][0] as $container |
    $config["current-context"] == $target and
    ($config.contexts | length) == 1 and $config.contexts[0].name == $target and
    $config.contexts[0].context.cluster == $target and
    ($config.clusters | length) == 1 and $config.clusters[0].name == $target and
    $container.State.Running == true and
    $container.Config.Labels["io.x-k8s.kind.cluster"] == $cluster and
    $container.Config.Labels["io.x-k8s.kind.role"] == "control-plane" and
    any($container.NetworkSettings.Ports["6443/tcp"][];
      .HostIp == "127.0.0.1" and
      $config.clusters[0].cluster.server == ("https://127.0.0.1:" + .HostPort)))
' >/dev/null || fail 'kubeconfig does not identify the job-created local Kind container'

for controller in source-controller helm-controller; do
	kubectl rollout status "deployment/$controller" --namespace flux-system --timeout=180s "${target[@]}"
	kubectl get "deployment/$controller" --namespace flux-system --output json "${target[@]}" >"$log_dir/$controller.json"
	selector=$(jq -er '.spec.selector.matchLabels | to_entries |
    map(.key + "=" + .value) | join(",") | select(length > 0)' "$log_dir/$controller.json")
	kubectl get pods --namespace flux-system --selector "$selector" --output json "${target[@]}" >"$log_dir/$controller-pods.json"
	jq -e '.items | length > 0 and all(.[];
    .status.phase == "Running" and
    (.status.containerStatuses | length) > 0 and
    all(.status.containerStatuses[]; .ready == true and (.imageID | length) > 0))' \
		"$log_dir/$controller-pods.json" >/dev/null || fail "$controller has no ready native image observation"
done
helm version --short >"$log_dir/helm-version.txt"

namespace_prefix="ksail-values-$(date +%s)-$RANDOM"
created_namespaces=()
created_uids=()
cleanup() {
	local status=$? cleanup_failed=0 namespace uid current_uid
	trap - EXIT
	for ((position = 0; position < ${#created_namespaces[@]}; position++)); do
		namespace="${created_namespaces[position]}"
		uid="${created_uids[position]}"
		current_uid=$(kubectl get namespace "$namespace" --ignore-not-found --output json "${target[@]}" |
			jq -r '.metadata.uid // empty') || {
			cleanup_failed=1
			continue
		}
		[[ -n "$current_uid" ]] || continue
		[[ "$current_uid" == "$uid" ]] || {
			echo "Refusing cleanup of replaced namespace $namespace"
			cleanup_failed=1
			continue
		}
		kubectl get helmreleases,helmrepositories,helmcharts,pods,events --namespace "$namespace" \
			--output json "${target[@]}" >"$log_dir/$namespace-diagnostics.json" || cleanup_failed=1
		kubectl delete namespace "$namespace" --wait=false "${target[@]}" || cleanup_failed=1
	done
	for namespace in "${created_namespaces[@]}"; do
		kubectl wait --for=delete "namespace/$namespace" --timeout=180s "${target[@]}" || cleanup_failed=1
	done
	if [[ $cleanup_failed != 0 && $status == 0 ]]; then status=1; fi
	exit "$status"
}
trap cleanup EXIT

create_namespace() {
	local namespace="$1" uid
	kubectl create namespace "$namespace" --output json "${target[@]}" >"$log_dir/$namespace-created.json"
	uid=$(jq -er '.metadata.uid | select(type == "string" and length > 0)' "$log_dir/$namespace-created.json")
	created_namespaces+=("$namespace")
	created_uids+=("$uid")
}

# Serve only checked-in chart bytes inside this cluster. The mirrored image is
# already used by the existing cluster action; no chart repository is contacted.
server_namespace="$namespace_prefix-server"
create_namespace "$server_namespace"
chart_dir="$log_dir/chart"
mkdir -p "$chart_dir"
helm package "$GITHUB_WORKSPACE/pkg/svc/gitops/render/testdata/values-precedence" --destination "$chart_dir"
chart_url="http://chart-server.$server_namespace.svc.cluster.local:8080"
helm repo index "$chart_dir" --url "$chart_url"
kubectl create configmap chart-files --namespace "$server_namespace" \
	--from-file="$chart_dir/index.yaml" --from-file="$chart_dir/values-probe-0.1.0.tgz" "${target[@]}"
cat >"$log_dir/chart-server.yaml" <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: chart-server
  namespace: $server_namespace
  labels:
    app: chart-server
spec:
  automountServiceAccountToken: false
  containers:
    - name: http
      image: mirror.gcr.io/library/busybox:1.37
      command: [httpd, -f, -p, "8080", -h, /chart]
      ports:
        - containerPort: 8080
      readinessProbe:
        httpGet:
          path: /index.yaml
          port: 8080
        initialDelaySeconds: 1
        periodSeconds: 2
      resources:
        requests: {cpu: 10m, memory: 8Mi}
        limits: {cpu: 100m, memory: 32Mi}
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        runAsNonRoot: true
        runAsUser: 1000
        capabilities: {drop: [ALL]}
        seccompProfile: {type: RuntimeDefault}
      volumeMounts:
        - name: chart
          mountPath: /chart
          readOnly: true
  volumes:
    - name: chart
      configMap: {name: chart-files}
---
apiVersion: v1
kind: Service
metadata:
  name: chart-server
  namespace: $server_namespace
spec:
  selector: {app: chart-server}
  ports:
    - port: 8080
      targetPort: 8080
YAML
kubectl create --filename "$log_dir/chart-server.yaml" "${target[@]}"
kubectl wait --for=condition=Ready pod/chart-server --namespace "$server_namespace" --timeout=180s "${target[@]}"

create_values() {
	local kind="$1" namespace="$2" name="$3" directory="$4" file
	local files=()
	for file in "$directory"/*; do files+=(--from-file="$file"); done
	if [[ "$kind" == ConfigMap ]]; then
		kubectl create configmap "$name" --namespace "$namespace" "${files[@]}" "${target[@]}"
	else
		kubectl create secret generic "$name" --namespace "$namespace" "${files[@]}" "${target[@]}"
	fi
}

run_case() {
	local kind="$1" family="$2" case_index="$3" namespace directory refs inline expected
	namespace="$namespace_prefix-$case_index"
	directory="$log_dir/$case_index-$kind-$family"
	mkdir -p "$directory/first" "$directory/second" "$directory/target"
	create_namespace "$namespace"
	printf '%s' 'replicaCount: 2
shared: {first: true, leaf: first}
nested: {keep: 7, flag: true, zero: 8, empty: old, list: [1]}
' >"$directory/first/values.yaml"
	printf '%s' 'replicaCount: 4
shared: {second: true, leaf: second}
nested: {flag: true, zero: 9, empty: later, list: [2]}
' >"$directory/second/values.yaml"
	printf 4 >"$directory/target/count"
	inline='{"replicaCount":3}'
	case "$family" in
	ordinary)
		inline='{"replicaCount":3,"shared":{"inline":true},"nested":{"flag":false,"zero":0,"empty":"","list":[]}}'
		expected='{"replicaCount":3,"shared":{"first":true,"second":true,"leaf":"second","inline":true},"nested":{"keep":7,"flag":false,"zero":0,"empty":"","list":[]}}'
		refs=$(jq -cn --arg kind "$kind" '[{kind:$kind,name:"first",literal:true},{kind:$kind,name:"second"}]')
		;;
	target-first | target-last | optional-target)
		# Keep order cases independent of other chart values and YAML merge tests.
		printf 'replicaCount: 2\notherCount: 2\n' >"$directory/first/values.yaml"
		if [[ "$family" == target-first ]]; then
			expected='{"replicaCount":3,"otherCount":2}'
			refs=$(jq -cn --arg kind "$kind" '[{kind:$kind,name:"target",valuesKey:"count",targetPath:"replicaCount"},{kind:$kind,name:"target",valuesKey:"count",targetPath:"otherCount"},{kind:$kind,name:"first"}]')
		elif [[ "$family" == target-last ]]; then
			expected='{"replicaCount":3,"otherCount":4}'
			refs=$(jq -cn --arg kind "$kind" '[{kind:$kind,name:"first"},{kind:$kind,name:"target",valuesKey:"count",targetPath:"replicaCount"},{kind:$kind,name:"target",valuesKey:"count",targetPath:"otherCount"}]')
		else
			expected='{"replicaCount":3,"otherCount":2}'
			refs=$(jq -cn --arg kind "$kind" '[{kind:$kind,name:"absent",valuesKey:"count",targetPath:"replicaCount",optional:true},{kind:$kind,name:"first"}]')
		fi
		;;
	literal)
		printf '%s\n%s' '{"token":"a,b=c","path":"C:\\tmp\\probe"}' 'second=line' >"$directory/target/payload"
		printf false >"$directory/target/false"
		printf 0 >"$directory/target/zero"
		printf null >"$directory/target/null"
		: >"$directory/target/empty"
		inline='{}'
		expected=$(jq -cn --rawfile payload "$directory/target/payload" \
			'{replicaCount:1,payload:$payload,literalFalse:"false",literalZero:"0",literalNull:"null",literalEmpty:""}')
		refs=$(jq -cn --arg kind "$kind" '[
        {kind:$kind,name:"target",valuesKey:"payload",targetPath:"payload",literal:true},
        {kind:$kind,name:"target",valuesKey:"false",targetPath:"literalFalse",literal:true},
        {kind:$kind,name:"target",valuesKey:"zero",targetPath:"literalZero",literal:true},
        {kind:$kind,name:"target",valuesKey:"null",targetPath:"literalNull",literal:true},
        {kind:$kind,name:"target",valuesKey:"empty",targetPath:"literalEmpty",literal:true}]')
		;;
	*) fail 'unknown trial case' ;;
	esac
	create_values "$kind" "$namespace" first "$directory/first"
	create_values "$kind" "$namespace" second "$directory/second"
	create_values "$kind" "$namespace" target "$directory/target"
	jq -n --arg namespace "$namespace" --arg url "$chart_url" '{
    apiVersion:"source.toolkit.fluxcd.io/v1",kind:"HelmRepository",
    metadata:{name:"values-probe",namespace:$namespace},spec:{interval:"1m",url:$url}}
  ' >"$directory/repository.json"
	kubectl create --filename "$directory/repository.json" "${target[@]}"
	jq -n --arg namespace "$namespace" --argjson refs "$refs" --argjson inline "$inline" '{
    apiVersion:"helm.toolkit.fluxcd.io/v2",kind:"HelmRelease",
    metadata:{name:"values-probe",namespace:$namespace},spec:{
      interval:"1m",timeout:"2m",releaseName:"values-probe",
      chart:{spec:{chart:"values-probe",version:"0.1.0",sourceRef:{kind:"HelmRepository",name:"values-probe"}}},
      valuesFrom:$refs,values:$inline}}
  ' >"$directory/release-spec.json"
	kubectl create --filename "$directory/release-spec.json" --output json "${target[@]}" >"$directory/created-release.json"
	jq -e --argjson values "$expected" '{namespace:.metadata.namespace,release:.metadata.name,
    uid:.metadata.uid,generation:.metadata.generation,values:$values}' \
		"$directory/created-release.json" >"$directory/expected.json"
	kubectl wait --for=condition=Ready helmrelease/values-probe --namespace "$namespace" --timeout=300s "${target[@]}"
	kubectl get helmrelease/values-probe --namespace "$namespace" --output json "${target[@]}" >"$directory/observed-release.json"
	kubectl get configmap/values-probe --namespace "$namespace" --output json "${target[@]}" >"$directory/observed-child.json"
	verify_observation "$directory/expected.json" "$directory/observed-release.json" "$directory/observed-child.json"
	echo "Native Flux values confirmed: $kind / $family"
}

case_index=0
for kind in ConfigMap Secret; do
	for family in ordinary target-first target-last optional-target literal; do
		case_index=$((case_index + 1))
		run_case "$kind" "$family" "$case_index"
	done
done
[[ $case_index == 10 ]] || fail 'native parity case coverage is incomplete'
echo 'Native Flux values precedence: all ten cases reconciled and matched literal expectations'
