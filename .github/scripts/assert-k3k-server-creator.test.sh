#!/usr/bin/env bash
#
# RED/GREEN proof for assert-k3k-server-creator.sh. Each fixture is a
# `kubectl get pods -o json` list; the positive control passes, and every
# negative control must be REJECTED with a message naming what was found.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
assert="${script_dir}/assert-k3k-server-creator.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

cluster="nested-k3s"

# pod_json NAME OWNER_KIND MANAGERS... — one server pod. MANAGERS are
# "<manager>[:<subresource>]" entries, all recorded as operation Update.
pod_json() {
	local name="$1" owner_kind="$2"
	shift 2

	local managed=""
	local entry manager subresource
	for entry in "$@"; do
		manager="${entry%%:*}"
		subresource=""
		case "${entry}" in
		*:*) subresource="${entry#*:}" ;;
		esac
		managed="${managed}{\"manager\":\"${manager}\",\"operation\":\"Update\",\"apiVersion\":\"v1\",\"time\":\"2026-09-05T00:00:00Z\",\"fieldsType\":\"FieldsV1\",\"fieldsV1\":{}"
		if [ -n "${subresource}" ]; then
			managed="${managed},\"subresource\":\"${subresource}\""
		fi
		managed="${managed}},"
	done
	managed="${managed%,}"

	local owners='[]'
	if [ -n "${owner_kind}" ]; then
		owners="[{\"apiVersion\":\"apps/v1\",\"kind\":\"${owner_kind}\",\"name\":\"k3k-${cluster}-server\",\"uid\":\"u\",\"controller\":true,\"blockOwnerDeletion\":true}]"
	fi

	printf '{"metadata":{"name":"%s","namespace":"k3k-%s","labels":{"role":"server","cluster":"%s"},"ownerReferences":%s,"managedFields":[%s]}}' \
		"${name}" "${cluster}" "${cluster}" "${owners}" "${managed}"
}

# list_json POD_JSON... — wraps pods into a kubectl List.
list_json() {
	local items
	items="$(printf '%s,' "$@")"
	printf '{"apiVersion":"v1","kind":"List","items":[%s]}' "${items%,}"
}

good_pod="$(pod_json "k3k-${cluster}-server-0" StatefulSet kube-controller-manager kubelet:status)"

fixture() {
	local name="$1"
	shift
	local file="${tmp_dir}/${name}.json"
	list_json "$@" >"${file}"
	printf '%s' "${file}"
}

expect_pass() {
	local label="$1" file="$2" evidence="${tmp_dir}/evidence-$1"
	if ! KSAIL_K3K_SERVER_POD_JSON="${file}" "${assert}" "${cluster}" "${evidence}" >"${tmp_dir}/${label}.out" 2>&1; then
		printf 'FAIL: %s was rejected:\n' "${label}" >&2
		cat "${tmp_dir}/${label}.out" >&2
		exit 1
	fi
	[ -s "${evidence}/k3k-server-pod-creator.json" ] || {
		printf 'FAIL: %s wrote no evidence file\n' "${label}" >&2
		exit 1
	}
}

expect_fail() {
	local label="$1" file="$2" needle="$3" evidence="${tmp_dir}/evidence-$1"
	if KSAIL_K3K_SERVER_POD_JSON="${file}" "${assert}" "${cluster}" "${evidence}" >"${tmp_dir}/${label}.out" 2>&1; then
		printf 'FAIL: %s passed the assertion\n' "${label}" >&2
		exit 1
	fi
	if ! grep -qF -- "${needle}" "${tmp_dir}/${label}.out"; then
		printf 'FAIL: %s was rejected without naming %q:\n' "${label}" "${needle}" >&2
		cat "${tmp_dir}/${label}.out" >&2
		exit 1
	fi
}

# GREEN — the premise the guard depends on: a StatefulSet-owned pod written by
# kube-controller-manager (the kubelet writes only the status subresource).
expect_pass "statefulset-owned" "$(fixture good "${good_pod}")"

# The evidence must record what the assertion read, so a later reader can
# re-derive it without a cluster.
evidence="${tmp_dir}/evidence-statefulset-owned/k3k-server-pod-creator.json"
jq -e '.namespace == "k3k-nested-k3s"
	and .pods[0].ownerReferences[0].kind == "StatefulSet"
	and ([.pods[0].managedFields[].manager] | index("kube-controller-manager") != null)
	and (.pods[0].managedFields | map(has("fieldsV1")) | any | not)' "${evidence}" >/dev/null

# RED — owner is a ReplicaSet (a Deployment-shaped k3k server would break the guard).
expect_fail "replicaset-owned" \
	"$(fixture rs "$(pod_json "k3k-${cluster}-server-0" ReplicaSet kube-controller-manager)")" \
	"controlled by [ReplicaSet]"

# RED — no controller owner at all (a bare pod created by some other actor).
expect_fail "unowned" \
	"$(fixture unowned "$(pod_json "k3k-${cluster}-server-0" "" kube-controller-manager)")" \
	"controlled by [none]"

# RED — a StatefulSet owner but a different writer: a k3k controller creating the
# pod itself would carry its own manager and never satisfy the pinned principal.
expect_fail "foreign-manager" \
	"$(fixture foreign "$(pod_json "k3k-${cluster}-server-0" StatefulSet k3k-controller kubelet:status)")" \
	"written by managers [k3k-controller]"

# RED — kube-controller-manager appears only on a subresource write; the pod
# object itself was written by someone else, so the creator is not proven.
expect_fail "kcm-status-only" \
	"$(fixture kcmstatus "$(pod_json "k3k-${cluster}-server-0" StatefulSet k3k-controller kube-controller-manager:status)")" \
	"written by managers [k3k-controller]"

# RED — no pod matched the labels: the guard's label assumptions no longer hold.
expect_fail "no-pods" "$(fixture empty)" "no pod with labels role=server,cluster=${cluster}"

# RED — every server pod is checked, not only the first (multi-replica servers).
expect_fail "second-pod-bad" \
	"$(fixture second "${good_pod}" "$(pod_json "k3k-${cluster}-server-1" ReplicaSet kube-controller-manager)")" \
	"k3k-${cluster}-server-1 is controlled by [ReplicaSet]"

# Live path — the real read goes through kubectl with the namespace and label
# selector the guard's exemption is written against. A fake kubectl records
# its arguments and serves the GREEN fixture.
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"
cat >"${fake_bin}/kubectl" <<FAKE
#!/usr/bin/env bash
printf '%s\n' "\$*" >"${tmp_dir}/kubectl.args"
cat "${tmp_dir}/good.json"
FAKE
chmod +x "${fake_bin}/kubectl"

if ! KUBECTL="${fake_bin}/kubectl" "${assert}" "${cluster}" "${tmp_dir}/evidence-live" >"${tmp_dir}/live.out" 2>&1; then
	printf 'FAIL: live path rejected the GREEN fixture:\n' >&2
	cat "${tmp_dir}/live.out" >&2
	exit 1
fi
expected_args="get pods -n k3k-${cluster} -l role=server,cluster=${cluster} -o json"
if [ "$(cat "${tmp_dir}/kubectl.args")" != "${expected_args}" ]; then
	printf 'FAIL: kubectl was invoked as %q, expected %q\n' "$(cat "${tmp_dir}/kubectl.args")" "${expected_args}" >&2
	exit 1
fi

# Usage guard: a missing argument is an error, never a silent pass.
if "${assert}" "${cluster}" >/dev/null 2>&1; then
	printf 'FAIL: missing evidence-dir argument passed\n' >&2
	exit 1
fi

printf 'OK: assert-k3k-server-creator.sh — 1 positive control, 7 negative controls, live-path arguments pinned\n'
