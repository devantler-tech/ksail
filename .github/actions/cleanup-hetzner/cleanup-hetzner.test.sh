#!/usr/bin/env bash
#
# Hermetic tests for cleanup-hetzner.sh (#7435). A fake `hcloud` serves each resource kind from a
# state file (one ID per line), removes an ID on a successful delete, and fails any call whose
# "<kind> <verb> [<id>]" prefix is listed in FAKE_HCLOUD_FAIL. A fake `sleep` returns immediately,
# so no case touches the network. A failed call must never be reported as a clean cleanup, and one
# failure must never stop the remaining kinds from being cleaned.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cleanup="${CLEANUP_HETZNER_SCRIPT:-${script_dir}/cleanup-hetzner.sh}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

cat >"${fake_bin}/hcloud" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
kind="$1" verb="$2" id=""
[[ "${verb}" == list ]] || id="${3:-}"
shift 2
printf '%s %s %s\n' "${kind}" "${verb}" "${id}" >>"${FAKE_HCLOUD_STATE}/calls"
call="${kind} ${verb}${id:+ ${id}}"
while IFS= read -r rule; do
	[[ -z "${rule}" ]] && continue
	if [[ "${call}" == "${rule}" ]]; then
		echo "hcloud: ${call} failed (simulated)" >&2
		exit 1
	fi
	# A "once:" rule fails only the first matching call.
	if [[ "once:${call}" == "${rule}" && ! -e "${FAKE_HCLOUD_STATE}/once-${kind}-${verb}-${id}" ]]; then
		touch "${FAKE_HCLOUD_STATE}/once-${kind}-${verb}-${id}"
		echo "hcloud: ${call} failed once (simulated)" >&2
		exit 1
	fi
done <<<"${FAKE_HCLOUD_FAIL:-}"
state="${FAKE_HCLOUD_STATE}/${kind}"
case "${verb}" in
list)
	echo "warning: simulated deprecation notice" >&2
	[[ -f "${state}" ]] || exit 0
	selector=""
	while [[ "$#" -gt 0 ]]; do
		case "$1" in
		-l)
			selector="$2"
			shift 2
			;;
		*) shift ;;
		esac
	done
	while IFS= read -r resource_id; do
		[[ -n "${resource_id}" ]] || continue
		labels=""
		while IFS='|' read -r labelled_id resource_labels; do
			if [[ "${labelled_id}" == "${resource_id}" ]]; then
				labels="${resource_labels}"
				break
			fi
		done <"${state}.labels"
		matches=1
		terms="${selector},"
		while [[ -n "${terms}" ]]; do
			term="${terms%%,*}"
			terms="${terms#*,}"
			[[ -n "${term}" ]] || continue
			if [[ ",${labels}," != *",${term},"* ]]; then
				matches=0
				break
			fi
		done
		[[ "${matches}" -eq 0 ]] || printf '%s\n' "${resource_id}"
	done <"${state}"
	;;
delete)
	# A provider can acknowledge a deletion before the resource disappears.
	# A successful command alone must not be accepted as absence evidence.
	[[ "${FAKE_HCLOUD_KEEP:-}" != "${kind}" ]] || exit 0
	grep -vx -- "${id}" "${state}" >"${state}.next" || true
	mv "${state}.next" "${state}"
	;;
esac
exit 0
FAKE

printf '#!/usr/bin/env bash\nexit 0\n' >"${fake_bin}/sleep"
chmod +x "${fake_bin}/hcloud" "${fake_bin}/sleep"

pass_count=0

# mismatch NAME WHAT WANT GOT OUTPUT — report one failed expectation of a case, with its output.
mismatch() {
	printf 'FAIL: %s: %s: wanted %q, got %q\n--- output ---\n%s\n' "$1" "$2" "$3" "$4" "$5" >&2
	return 1
}

# run_case NAME EXPECTED_STATUS EXPECTED_TEXT FAIL_RULES RESOURCES... — each RESOURCE is
# "<kind>=<id>[,<id>...]". FAIL_RULES is newline-separated. After the run, every case asserts that
# CASE_REMAINING (default: nothing) is exactly what is left across all kinds.
run_case() {
	local name="$1" expected_status="$2" expected_text="$3" fail_rules="$4"
	shift 4

	local state="${tmp_dir}/${name}"
	mkdir -p "${state}"
	local resource kind ids id
	for resource in "$@"; do
		kind="${resource%%=*}"
		ids="${resource#*=}"
		tr ',' '\n' <<<"${ids}" >"${state}/${kind}"
		while IFS= read -r id; do
			printf '%s|ksail.cluster.name=test,ksail.owned=true\n' "${id}"
		done <"${state}/${kind}" >"${state}/${kind}.labels"
	done
	if [[ -n "${CASE_FLOATING_IP_LABELS:-}" ]]; then
		printf '%s\n' "${CASE_FLOATING_IP_LABELS}" >"${state}/floating-ip.labels"
	fi

	local output status=0
	output="$(PATH="${fake_bin}:${PATH}" FAKE_HCLOUD_STATE="${state}" FAKE_HCLOUD_FAIL="${fail_rules}" \
		FAKE_HCLOUD_KEEP="${CASE_KEEP_KIND:-}" \
		LABEL_SELECTOR="ksail.cluster.name=test" bash "${cleanup}" 2>&1)" || status=$?

	local remaining
	remaining="$(cd "${state}" && for kind in server floating-ip placement-group firewall network; do
		if [[ -s "${kind}" ]]; then sed "s/^/${kind}=/" "${kind}"; fi
	done | tr '\n' ' ')"

	[[ "${status}" -eq "${expected_status}" ]] ||
		mismatch "${name}" "exit status" "${expected_status}" "${status}" "${output}"
	[[ "${output}" == *"${expected_text}"* ]] ||
		mismatch "${name}" "output fragment" "${expected_text}" "(absent)" "${output}"
	[[ "${remaining}" == "${CASE_REMAINING:-}" ]] ||
		mismatch "${name}" "resources left" "${CASE_REMAINING:-}" "${remaining}" "${output}"

	pass_count=$((pass_count + 1))
}

run_case nothing-to-delete 0 "✅ Hetzner Cloud cleanup complete!" ""

run_case deletes-every-kind 0 "✅ Hetzner Cloud cleanup complete!" "" \
	server=11,12 floating-ip=21 placement-group=31 firewall=41 network=51

for retained_kind in server floating-ip placement-group network; do
	CASE_KEEP_KIND="${retained_kind}" CASE_REMAINING="${retained_kind}=11 " \
		run_case "acknowledged-${retained_kind}-still-present" 1 \
		"❌ ${retained_kind} resources still present after cleanup: 11" "" "${retained_kind}=11"
done

run_case unreachable-api 1 "❌ Failed to access Hetzner Cloud: hcloud: location list failed" \
	"location list"

# The core of #7435: a failed list is a failure, and the later kinds are still cleaned.
run_case server-list-fails 1 "❌ Failed to list server resources: hcloud: server list failed" \
	"server list" floating-ip=21 network=51

CASE_REMAINING="server=11 " run_case server-delete-fails 1 "❌ Failed to delete server 11" \
	"server delete 11" server=11,12 network=51

run_case assigned-floating-ip 0 "Unassigning floating-ip 21 before retrying" \
	"once:floating-ip delete 21" floating-ip=21

CASE_REMAINING="floating-ip=21 " run_case floating-ip-unassign-fails 1 "❌ Failed to delete floating-ip 21" \
	"floating-ip delete 21
floating-ip unassign 21" floating-ip=21 network=51

run_case firewall-detaches-late 0 "✅ Hetzner Cloud cleanup complete!" \
	"once:firewall delete 41" firewall=41

CASE_REMAINING="firewall=41 " run_case firewall-never-detaches 1 "❌ Firewalls still present after 4 attempts: 41" \
	"firewall delete 41" firewall=41 network=51

run_case firewall-recheck-fails 1 "❌ Failed to list firewall resources" \
	"firewall list" network=51

if ! grep -q 'network delete 51' "${tmp_dir}/firewall-recheck-fails/calls"; then
	echo 'FAIL: firewall-recheck-fails: networks were not cleaned after the firewall list failed' >&2
	exit 1
fi
pass_count=$((pass_count + 1))

CASE_FLOATING_IP_LABELS='21|ksail.owned=true,ksail.cluster.name=test
22|ksail.cluster.name=test
23|ksail.owned=true,ksail.cluster.name=other
24|ksail.owned=false,ksail.cluster.name=test' \
	CASE_REMAINING='floating-ip=22 floating-ip=23 floating-ip=24 ' \
	run_case preserves-unowned-floating-ips 0 "Unassigning floating-ip 21 before retrying" \
	"once:floating-ip delete 21" floating-ip=21,22,23,24

if grep -Eq '^floating-ip (delete|unassign) (22|23|24)$' "${tmp_dir}/preserves-unowned-floating-ips/calls"; then
	echo 'FAIL: cleanup modified a floating IP outside its ownership and cluster scope' >&2
	exit 1
fi
pass_count=$((pass_count + 1))

if ! grep -q '^floating-ip unassign 21$' "${tmp_dir}/preserves-unowned-floating-ips/calls"; then
	echo 'FAIL: the selected owned floating IP did not exercise unassignment/retry' >&2
	exit 1
fi
pass_count=$((pass_count + 1))

echo "PASS: ${pass_count} cleanup-hetzner cases"
