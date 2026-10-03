#!/usr/bin/env bash
# Exercise the actual cert-manager values reconciliation on an existing CI cluster.
set -euo pipefail

fail() {
	echo "cert-manager values trial: $*" >&2
	exit 1
}
log_dir="${SYSTEM_TEST_LOG_DIR:-/tmp/ksail-system-test-logs}/cert-manager-values-drift"
mkdir -p "$log_dir"
command -v helm >/dev/null || fail 'Helm CLI is required to seed the live release'
helm version --short >"$log_dir/helm-version.txt"
cp ksail.yaml "$log_dir/ksail-before.yaml"

# Keep the create-time Kubernetes pin; changing upstream tags must not turn this
# values-only trial into a distribution upgrade. Init-only flags are not accepted.
update_args_text=$(printf '%s' "$ARGS" | sed 's/--image-verification [^ ]*//g')
read -r -a update_args <<<"$update_args_text"
read -r -a version_args <<<"$K8S_VERSION_FLAG"
[[ ${#version_args[@]} == 2 && ${version_args[0]} == --kubernetes-version &&
	${version_args[1]} =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || fail 'missing create-time Kubernetes version pin'

release_state() {
	local phase="$1"
	helm list --all --namespace cert-manager --output json >"$log_dir/$phase-release.json" || fail 'could not read latest Helm release'
	# --all includes the latest failed/pending revision. A deployed-only query could
	# hide a second attempted upgrade and falsely report convergence.
	jq -er 'map(select(.name == "cert-manager")) |
    select(length == 1) | .[0] |
    select(.status == "deployed" and (.revision | tostring | test("^[1-9][0-9]*$")) and
      (.chart | test("^cert-manager-v?[0-9]+\\.[0-9]+\\.[0-9]+([-+][0-9A-Za-z.-]+)?$"))) |
    [.revision | tostring] | .[0]' "$log_dir/$phase-release.json" || fail 'latest release is not a single deployed revision'
}

read_values() {
	local phase="$1" revision="$2"
	helm get values cert-manager --namespace cert-manager --revision "$revision" --output json |
		jq -eS 'select(type == "object")' >"$log_dir/$phase-values.json" || fail 'release values are not an object'
}

assert_ownership() {
	local revision="$1"
	kubectl get secrets --namespace cert-manager --selector owner=helm,name=cert-manager \
		--request-timeout=30s --output json >"$log_dir/storage.json"
	jq -e --arg revision "$revision" '
    .items | map(.metadata.labels) |
    select(length > 0 and all(.[]; (.version | test("^[1-9][0-9]*$")))) |
    max_by(.version | tonumber) |
    .version == $revision and .status == "deployed" and
    (has("helm.toolkit.fluxcd.io/name") | not) and
    (has("argocd.argoproj.io/managed-by") | not)
  ' "$log_dir/storage.json" >/dev/null || fail 'latest storage revision differs or is GitOps-owned'
}

assert_diff() {
	local phase="$1" expected="$2" status=0
	ksail cluster diff --output json --exit-code --distribution "$DISTRIBUTION" \
		--provider "$PROVIDER" "${update_args[@]}" >"$log_dir/$phase.stdout" 2>"$log_dir/$phase.stderr" || status=$?
	if [[ "$expected" == 1 ]]; then
		[[ "$status" == 2 ]] || fail 'values drift did not exit exactly 2'
		assert_values_change "$log_dir/$phase.stdout"
	else
		[[ "$status" == 0 ]] || fail 'repeated diff did not exit exactly 0'
		assert_no_changes "$log_dir/$phase.stdout"
	fi
}

assert_values_change() {
	jq -e -s 'length == 1 and (.[0] |
    type == "object" and .totalChanges == 1 and
    (.inPlaceChanges | length == 1) and
    .inPlaceChanges[0].field == "cluster.certManager.chartValues" and
    .inPlaceChanges[0].category == "in-place" and
    .rebootRequired == [] and .recreateRequired == [] and .rollingRecreate == [] and
    .wipeRequired == [] and .unknownBaseline == [])' "$1" >/dev/null || fail 'expected only one in-place cert-manager values change'
}

assert_no_changes() {
	jq -e -s 'length == 1 and (.[0] |
    type == "object" and .totalChanges == 0 and .inPlaceChanges == [] and
    .rebootRequired == [] and .recreateRequired == [] and .rollingRecreate == [] and
    .wipeRequired == [] and .unknownBaseline == [])' "$1" >/dev/null || fail 'expected exactly one JSON document with no changes'
}

update_cluster() {
	local phase="$1"
	ksail cluster update --force --output json --distribution "$DISTRIBUTION" \
		--provider "$PROVIDER" "${update_args[@]}" "${version_args[@]}" \
		>"$log_dir/$phase.stdout" 2>"$log_dir/$phase.stderr" || fail 'cluster update failed'
}

original_revision=$(release_state original)
assert_ownership "$original_revision"
read_values original "$original_revision"
jq -e '.installCRDs == true and (.startupapicheck.timeout | type == "string")' \
	"$log_dir/original-values.json" >/dev/null || fail 'initial KSail values are missing'
assert_diff initial 0
chart_version=$(jq -er 'map(select(.name == "cert-manager")) | .[0].chart |
  sub("^cert-manager-"; "")' "$log_dir/original-release.json")
seed_timeout=12m0s
if [[ $(jq -r '.startupapicheck.timeout' "$log_dir/original-values.json") == "$seed_timeout" ]]; then
	seed_timeout=13m0s
fi
jq -eS --arg timeout "$seed_timeout" '.startupapicheck.timeout = $timeout' \
	"$log_dir/original-values.json" >"$log_dir/expected-seed-values.json"

# Helm --set stores booleans as actual booleans, matching real release storage.
# Only the timeout changes; KSail's declared spec remains byte-for-byte identical.
helm upgrade cert-manager cert-manager --repo https://charts.jetstack.io \
	--namespace cert-manager --version "$chart_version" --reuse-values \
	--set installCRDs=true --set "startupapicheck.timeout=$seed_timeout" \
	--atomic --wait --wait-for-jobs --timeout 10m >"$log_dir/seed-upgrade.log" 2>&1
seed_revision=$(release_state seeded)
[[ "$seed_revision" == "$((original_revision + 1))" ]] || fail 'seeding did not create exactly one revision'
assert_ownership "$seed_revision"
read_values seeded "$seed_revision"
cmp -s "$log_dir/expected-seed-values.json" "$log_dir/seeded-values.json" || fail 'seed values do not match the intended drift'
cmp -s ksail.yaml "$log_dir/ksail-before.yaml" || fail 'seeding changed the declared KSail spec'
assert_diff drift 1

update_cluster reconcile
assert_values_change "$log_dir/reconcile.stdout"
updated_revision=$(release_state updated)
[[ "$updated_revision" == "$((seed_revision + 1))" ]] || fail 'reconciliation did not create exactly one revision'
assert_ownership "$updated_revision"
read_values updated "$updated_revision"
cmp -s "$log_dir/original-values.json" "$log_dir/updated-values.json" || fail 'reconciliation did not restore the desired values'
for deployment in cert-manager cert-manager-webhook cert-manager-cainjector; do
	kubectl rollout status "deployment/$deployment" --namespace cert-manager \
		--timeout=600s --request-timeout=30s >"$log_dir/$deployment-readiness.log" 2>&1 || fail "$deployment is not ready"
done

assert_diff repeated 0
update_cluster repeat-update
assert_no_changes "$log_dir/repeat-update.stdout"
grep -q 'No changes detected' "$log_dir/repeat-update.stderr" || fail 'repeated update omitted its no-op result'
repeated_revision=$(release_state repeated)
[[ "$repeated_revision" == "$updated_revision" ]] || fail 'repeated update changed the Helm revision'
assert_ownership "$repeated_revision"
read_values repeated "$repeated_revision"
cmp -s "$log_dir/original-values.json" "$log_dir/repeated-values.json" || fail 'repeated update changed desired values'
cmp -s ksail.yaml "$log_dir/ksail-before.yaml" || fail 'trial changed the declared KSail spec'
echo "cert-manager values trial: one Helm upgrade and a repeated no-op (revisions $seed_revision → $updated_revision → $repeated_revision)"
