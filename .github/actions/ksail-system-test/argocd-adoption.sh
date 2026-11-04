#!/usr/bin/env bash
# Prove that `ksail cluster update` leaves alone a Helm release an Argo CD Application
# has adopted (#8089, #8220). Argo CD writes no Helm record, so the release KSail
# installed stays unlabelled; KSail must find the Application through its tracked
# resources and skip the release instead of reconciling it back.
#
# Runs on the Docker leg that installs Gatekeeper with Argo CD, against the cluster the
# job just created (the current kubeconfig context) and the ksail.yaml it initialised.
#
# 1. Seed values drift on the KSail-installed gatekeeper release and confirm
#    `ksail cluster diff` reports it: the control showing an update would change it.
# 2. Adopt the release with an Argo CD Application (manual sync, no finalizer) and wait
#    until Argo CD tracks the release's controller Deployment.
# 3. Run `ksail cluster update` and require that the release is untouched: same
#    revision, the seeded values kept.
# 4. Remove the Application without cascading, so the release stays as it was.
set -euo pipefail

release=gatekeeper
namespace=gatekeeper-system
chart_repo=https://open-policy-agent.github.io/gatekeeper/charts
application=ksail-adoption-trial
argocd_namespace=argocd
tracked_deployment=gatekeeper-controller-manager

fail() {
	echo "Argo CD adoption trial: $*" >&2
	exit 1
}

log_dir="${SYSTEM_TEST_LOG_DIR:-/tmp/ksail-system-test-logs}/argocd-adoption"
mkdir -p "$log_dir"
config_file="$PWD/ksail.yaml"
[[ -f "$config_file" ]] || fail 'ksail.yaml from the initialised cluster is missing'
cp "$config_file" "$log_dir/ksail-before.yaml"

read -r -a version_args <<<"${K8S_VERSION_FLAG:-}"
[[ ${#version_args[@]} == 2 && ${version_args[0]} == --kubernetes-version ]] ||
	fail 'missing create-time Kubernetes version pin'

command -v helm >/dev/null || fail 'Helm CLI is required to seed the live release'
helm version --short >"$log_dir/helm-version.txt"
case "$(cat "$log_dir/helm-version.txt")" in
v3.*)
	helm_list_args=(--all --namespace "$namespace")
	helm_rollback_flag=--atomic
	helm_conflict_args=()
	;;
v4.*)
	helm_list_args=(--namespace "$namespace")
	helm_rollback_flag=--rollback-on-failure
	# KSail installs with server-side apply under its own field manager.
	helm_conflict_args=(--server-side=true --force-conflicts)
	;;
*) fail 'unsupported Helm major version' ;;
esac

# Prints "<revision> <chart version>" of the single deployed release.
release_state() {
	local phase="$1"
	helm list "${helm_list_args[@]}" --output json >"$log_dir/$phase-release.json" ||
		fail 'could not read the Helm release'
	jq -er --arg release "$release" 'map(select(.name == $release)) |
    select(length == 1) | .[0] |
    select(.status == "deployed" and (.revision | tostring | test("^[1-9][0-9]*$"))) |
    "\(.revision) \(.chart | ltrimstr($release + "-"))"' "$log_dir/$phase-release.json" ||
		fail "$phase: latest release is not a single deployed revision"
}

read_values() {
	local phase="$1" revision="$2"
	helm get values "$release" --namespace "$namespace" --revision "$revision" --output json |
		jq -eS 'select(type == "object")' >"$log_dir/$phase-values.json" ||
		fail "$phase: release values are not an object"
}

# shellcheck disable=SC2317 # Invoked by the EXIT trap.
cleanup() {
	local result=$?
	trap - EXIT
	if [[ "${application_created:-false}" == true ]]; then
		# No resources finalizer was set, so deleting the Application leaves its objects.
		kubectl delete application "$application" --namespace "$argocd_namespace" \
			--wait=true --timeout=60s >"$log_dir/cleanup.log" 2>&1 || {
			echo 'Argo CD adoption trial: could not remove the trial Application' >&2
			result=1
		}
	fi
	# Restore the release KSail installed, so later steps see the cluster as created.
	# Upgrade back to the original user values, the same way the seed was applied.
	if [[ "${seeded:-false}" == true ]] && ! helm upgrade "$release" "$release" --repo "$chart_repo" \
		--namespace "$namespace" --version "$chart_version" --reset-values --values "$log_dir/original-values.json" \
		"$helm_rollback_flag" ${helm_conflict_args[@]+"${helm_conflict_args[@]}"} \
		--wait --timeout 10m >"$log_dir/restore.log" 2>&1; then
		echo "Argo CD adoption trial: could not restore the original values; see $log_dir/restore.log" >&2
		result=1
	fi
	exit "$result"
}
trap cleanup EXIT

read -r original_revision chart_version <<<"$(release_state original)"
[[ "$chart_version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] ||
	fail 'could not read the installed chart version'
read_values original "$original_revision"
jq -e '.podAnnotations["ksail-adoption-trial"] == null' "$log_dir/original-values.json" >/dev/null ||
	fail 'seed value is already present'

# 1. Seed drift: a pod annotation KSail never renders.
helm upgrade "$release" "$release" --repo "$chart_repo" \
	--namespace "$namespace" --version "$chart_version" --reuse-values \
	--set podAnnotations.ksail-adoption-trial=seeded \
	"$helm_rollback_flag" ${helm_conflict_args[@]+"${helm_conflict_args[@]}"} \
	--wait --timeout 10m >"$log_dir/seed-upgrade.log" 2>&1 ||
	fail "seeding upgrade failed; see $log_dir/seed-upgrade.log"
seeded=true
read -r seed_revision _ <<<"$(release_state seeded)"
[[ "$seed_revision" == "$((original_revision + 1))" ]] || fail 'seeding did not create exactly one revision'
read_values seeded "$seed_revision"
jq -e '.podAnnotations["ksail-adoption-trial"] == "seeded"' "$log_dir/seeded-values.json" >/dev/null ||
	fail 'seeded value is missing from the release'

# Control: without an Application the drift is reported, so an update would upgrade it.
diff_status=0
ksail cluster diff --config "$config_file" --output json --exit-code \
	>"$log_dir/control-diff.stdout" 2>"$log_dir/control-diff.stderr" || diff_status=$?
[[ "$diff_status" == 2 ]] || fail "control diff did not report the seeded drift (exit $diff_status)"
jq -e -s 'length == 1 and (.[0].inPlaceChanges | any(.field == "cluster.policyEngine.chartValues"))' \
	"$log_dir/control-diff.stdout" >/dev/null || fail 'control diff did not name the Gatekeeper values'

# 2. Adopt the release with an Argo CD Application: manual sync, no resources finalizer.
jq -n --arg name "$application" --arg ns "$argocd_namespace" --arg repo "$chart_repo" \
	--arg chart "$release" --arg version "${chart_version#v}" --arg target "$namespace" '{
    apiVersion: "argoproj.io/v1alpha1", kind: "Application",
    metadata: {name: $name, namespace: $ns},
    spec: {
      project: "default",
      source: {repoURL: $repo, chart: $chart, targetRevision: $version, helm: {releaseName: $chart}},
      destination: {server: "https://kubernetes.default.svc", namespace: $target}
    }}' >"$log_dir/application.json"
kubectl create --filename "$log_dir/application.json" >"$log_dir/application-create.log" 2>&1 ||
	fail "could not create the trial Application; see $log_dir/application-create.log"
application_created=true

tracked=false
for _ in $(seq 1 60); do
	if kubectl get application "$application" --namespace "$argocd_namespace" --output json \
		>"$log_dir/application-status.json" 2>/dev/null &&
		jq -e --arg ns "$namespace" --arg name "$tracked_deployment" \
			'.status.resources // [] | any(.kind == "Deployment" and .name == $name and .namespace == $ns)' \
			"$log_dir/application-status.json" >/dev/null; then
		tracked=true
		break
	fi
	sleep 5
done
[[ "$tracked" == true ]] || fail 'Argo CD never tracked the Gatekeeper controller Deployment'

# 3. Update: the adopted release must be left alone.
ksail cluster update --config "$config_file" --yes "${version_args[@]}" \
	>"$log_dir/update.stdout" 2>"$log_dir/update.stderr" || fail "cluster update failed; see $log_dir/update.stderr"
read -r updated_revision _ <<<"$(release_state updated)"
[[ "$updated_revision" == "$seed_revision" ]] ||
	fail "cluster update changed the adopted release (revision $seed_revision → $updated_revision)"
read_values updated "$updated_revision"
cmp -s "$log_dir/seeded-values.json" "$log_dir/updated-values.json" ||
	fail 'cluster update changed the adopted release values'
cmp -s "$config_file" "$log_dir/ksail-before.yaml" || fail 'the trial changed the declared KSail spec'
if grep -q "release \"$release\" is managed by ArgoCD" "$log_dir/update.stderr"; then
	echo 'Argo CD adoption trial: update printed the skip notice for the adopted release'
fi

echo "Argo CD adoption trial: update left the adopted release at revision $seed_revision with its values"
