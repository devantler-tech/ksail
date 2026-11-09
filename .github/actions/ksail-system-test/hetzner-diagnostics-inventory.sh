#!/usr/bin/env bash
# Disposable diagnostic control. This helper only reads the existing test project.
set -euo pipefail
: "${HCLOUD_TOKEN:?Hetzner test credential is required}"

verify_absent() {
	local run="$1" kind selector ids
	local -a type_filter
	[[ "$run" =~ ^[0-9]+$ ]] || return 1
	for kind in server floating-ip placement-group firewall network load-balancer volume; do
		selector="ksail.cluster.name=st-hetzner-dispatch-$run"
		ids=$(timeout --kill-after=5s 12s hcloud "$kind" list -o noheader -o columns=id -l "$selector")
		[[ -z "$ids" ]] || { echo "Run $run still owns $kind resources" >&2; return 1; }
	done
	for kind in server image ssh-key; do
		selector="ksail.io/cluster=st-hetzner-dispatch-$run"
		type_filter=()
		if [[ "$kind" == image ]]; then
			type_filter=(--type snapshot)
		fi
		ids=$(timeout --kill-after=5s 12s hcloud "$kind" list -o noheader -o columns=id -l "$selector" "${type_filter[@]}")
		[[ -z "$ids" ]] || { echo "Run $run still owns snapshot-build $kind resources" >&2; return 1; }
	done
	echo "Verified run $run cluster and snapshot-build resource absence"
}

case "${1:-}" in
pre)
	[[ "${GITHUB_RUN_ATTEMPT:-}" == 1 ]] || exit 1
	[[ "${TRIAL_SERVER_TYPE:-}" == cx23 || "${TRIAL_SERVER_TYPE:-}" == cx33 ]] || exit 1
	case "${TRIAL_LOCATION:-}" in
	fsn1 | nbg1 | hel1) ;;
	*) exit 1 ;;
	esac
	shift
	for run in "$@" "$GITHUB_RUN_ID"; do
		verify_absent "$run"
	done
	timeout --kill-after=5s 12s hcloud server-type describe "$TRIAL_SERVER_TYPE" -o json |
		jq -e --arg type "$TRIAL_SERVER_TYPE" --arg location "$TRIAL_LOCATION" '
			.name == $type and
			([.locations[] | select(.name == $location)] | length == 1 and .[0].available == true)
		' >/dev/null
	echo "Verified selected server class is available in the trial location"
	;;
post) verify_absent "$GITHUB_RUN_ID" ;;
*) exit 1 ;;
esac
