#!/usr/bin/env bash
#
# RED/GREEN proof for assert-gitops-deployed.sh. A fake `ksail` serves the root object and the
# fixture ConfigMap; the positive controls pass, and every negative control must be REJECTED with
# a message naming what was found. The "manages 0 resources" controls are the #6284 shape: the
# engine reports success while deploying nothing.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
assert="${script_dir}/assert-gitops-deployed.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fixture="ksail-gitops-system-test"
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

# The fake ksail records its arguments and answers from files the case sets up:
#   ROOT_JSON    the root Kustomization/Application (absent => the read fails)
#   CONFIGMAP    "present" when the fixture ConfigMap exists in the cluster
cat >"${fake_bin}/ksail" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CASE_DIR}/ksail.args"
case "$*" in
"workload get kustomization flux-system -n flux-system -o json" | "workload get application ksail -n argocd -o json")
	[ -f "${CASE_DIR}/root.json" ] || { printf 'Error from server (NotFound)\n' >&2; exit 1; }
	cat "${CASE_DIR}/root.json"
	;;
"workload get configmap ksail-gitops-system-test -n default -o name")
	[ -f "${CASE_DIR}/configmap-present" ] || { printf 'Error from server (NotFound)\n' >&2; exit 1; }
	printf 'configmap/ksail-gitops-system-test\n'
	;;
*) printf 'unexpected ksail request: %s\n' "$*" >&2; exit 2 ;;
esac
FAKE
chmod +x "${fake_bin}/ksail"

flux_root() {
	local entries="$1"
	printf '{"kind":"Kustomization","status":{"inventory":{"entries":[%s]}}}' "${entries}"
}

argocd_root() {
	local resources="$1"
	printf '{"kind":"Application","status":{"sync":{"status":"Synced"},"health":{"status":"Healthy"},"resources":[%s]}}' "${resources}"
}

flux_fixture_entry="{\"id\":\"default_${fixture}__ConfigMap\",\"v\":\"v1\"}"
flux_other_entry='{"id":"default_whoami_apps_Deployment","v":"v1"}'
argocd_fixture_entry="{\"kind\":\"ConfigMap\",\"namespace\":\"default\",\"name\":\"${fixture}\",\"version\":\"v1\"}"
argocd_other_entry='{"kind":"Deployment","group":"apps","namespace":"default","name":"whoami","version":"v1"}'

# run_case LABEL ENGINE ROOT_JSON CONFIGMAP(present|absent) — run the assertion once, no waiting.
run_case() {
	local label="$1" engine="$2" root="$3" configmap="$4"
	local case_dir="${tmp_dir}/${label}"
	mkdir -p "${case_dir}"
	[ -z "${root}" ] || printf '%s' "${root}" >"${case_dir}/root.json"
	[ "${configmap}" != present ] || : >"${case_dir}/configmap-present"
	CASE_DIR="${case_dir}" KSAIL="${fake_bin}/ksail" ASSERT_GITOPS_ATTEMPTS=1 ASSERT_GITOPS_INTERVAL=0 \
		"${assert}" "${engine}" "${fixture}" >"${case_dir}/out" 2>&1
}

expect_pass() {
	local label="$1"
	if ! run_case "$@"; then
		printf 'FAIL: %s was rejected:\n' "${label}" >&2
		cat "${tmp_dir}/${label}/out" >&2
		exit 1
	fi
}

# expect_fail LABEL ENGINE ROOT_JSON CONFIGMAP NEEDLE — require rejection naming NEEDLE.
expect_fail() {
	local label="$1" needle="$5"
	if run_case "$1" "$2" "$3" "$4"; then
		printf 'FAIL: %s passed the assertion\n' "${label}" >&2
		exit 1
	fi
	if ! grep -qF -- "${needle}" "${tmp_dir}/${label}/out"; then
		printf 'FAIL: %s was rejected without naming %q:\n' "${label}" "${needle}" >&2
		cat "${tmp_dir}/${label}/out" >&2
		exit 1
	fi
}

# GREEN — each engine manages the pushed fixture, and it exists.
expect_pass flux-deployed Flux "$(flux_root "${flux_other_entry},${flux_fixture_entry}")" present
expect_pass argocd-deployed ArgoCD "$(argocd_root "${argocd_other_entry},${argocd_fixture_entry}")" present

# The live reads must target the root objects KSail creates.
grep -qxF 'workload get kustomization flux-system -n flux-system -o json' "${tmp_dir}/flux-deployed/ksail.args"
grep -qxF 'workload get application ksail -n argocd -o json' "${tmp_dir}/argocd-deployed/ksail.args"

# RED — #6284: Synced/Healthy (or Ready) while managing nothing.
expect_fail flux-empty Flux "$(flux_root "")" absent "manages 0 resources"
expect_fail argocd-empty ArgoCD "$(argocd_root "")" absent "manages 0 resources"

# RED — the engine manages something, but not what the test pushed (wrong path or artifact).
expect_fail flux-fixture-unmanaged Flux "$(flux_root "${flux_other_entry}")" present \
	"does not manage ConfigMap/default/${fixture}"
expect_fail argocd-fixture-unmanaged ArgoCD "$(argocd_root "${argocd_other_entry}")" present \
	"does not manage ConfigMap/default/${fixture}"

# RED — the root object claims the fixture, but the cluster does not have it.
expect_fail argocd-fixture-missing ArgoCD "$(argocd_root "${argocd_fixture_entry}")" absent \
	"but it does not exist in the cluster"

# RED — the root object cannot be read at all.
expect_fail flux-root-missing Flux "" absent "could not read Kustomization flux-system/flux-system"

# The failure output names what the engine actually managed.
grep -qF 'managed:  Deployment/default/whoami' "${tmp_dir}/flux-fixture-unmanaged/out"

# Usage guards: an unknown engine or a missing argument is an error, never a silent pass.
if "${assert}" Fleet "${fixture}" >/dev/null 2>&1; then
	printf 'FAIL: unknown engine passed\n' >&2
	exit 1
fi
if "${assert}" Flux >/dev/null 2>&1; then
	printf 'FAIL: missing fixture argument passed\n' >&2
	exit 1
fi

printf 'OK: GitOps deployment assertion accepts real deployments and rejects empty or wrong ones\n'
