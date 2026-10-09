#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS_FILE"
if [[ "$1 $2" == "cluster update" ]]; then
	if [[ " $* " == *" --dry-run "* ]]; then
		count=0
		[[ ! -f "$DRY_COUNT_FILE" ]] || count=$(<"$DRY_COUNT_FILE")
		count=$((count + 1))
		printf '%s' "$count" >"$DRY_COUNT_FILE"
		if [[ "$SCENARIO" == pre-missing && "$count" == 1 ]] ||
			[[ "$SCENARIO" != post-drift && "$count" == 2 ]]; then
			echo 'No changes detected'
		else
			printf 'Would reconcile distribution image at %s.\n' "$LIVE_TALOS_VERSION"
			if [[ "$SCENARIO" == pre-config-drift && "$count" == 1 ]]; then
				echo 'Would apply 1 in-place, 0 reboot-required, 0 recreate-required'
			else
				# Image reconciliation and the configuration summary are separate.
				# A still-drifted image can accompany a clean configuration plan.
				echo 'No changes detected'
			fi
		fi
	else
		printf 'Distribution image reconciled at %s.\n' "$LIVE_TALOS_VERSION"
	fi
elif [[ "$1 $2" == "cluster info" ]]; then
	echo 'Ready: 1/1 (ready/total)'
elif [[ "$1 $2 $3" == "workload get nodes" ]]; then
	case "$SCENARIO" in
	mixed-version)
		echo '{"items":[{"status":{"nodeInfo":{"osImage":"Talos (v1.13.10)"},"conditions":[{"type":"Ready","status":"True"}]}},{"status":{"nodeInfo":{"osImage":"Talos (v1.14.2)"},"conditions":[{"type":"Ready","status":"True"}]}}]}'
		;;
	missing-version)
		echo '{"items":[{"status":{"nodeInfo":{},"conditions":[{"type":"Ready","status":"True"}]}}]}'
		;;
	non-talos)
		echo '{"items":[{"status":{"nodeInfo":{"osImage":"Ubuntu 24.04"},"conditions":[{"type":"Ready","status":"True"}]}}]}'
		;;
	mixed-missing-version)
		echo '{"items":[{"status":{"nodeInfo":{"osImage":"Talos (v1.14.2)"},"conditions":[{"type":"Ready","status":"True"}]}},{"status":{"nodeInfo":{},"conditions":[{"type":"Ready","status":"True"}]}}]}'
		;;
	mixed-non-talos)
		echo '{"items":[{"status":{"nodeInfo":{"osImage":"Talos (v1.14.2)"},"conditions":[{"type":"Ready","status":"True"}]}},{"status":{"nodeInfo":{"osImage":"Ubuntu 24.04"},"conditions":[{"type":"Ready","status":"True"}]}}]}'
		;;
	no-nodes) echo '{"items":[]}' ;;
	nodes-query-failed) exit 9 ;;
	malformed-nodes) echo '{' ;;
	*)
		ready=True
		[[ "$SCENARIO" != not-ready ]] || ready=False
		printf '{"items":[{"status":{"nodeInfo":{"osImage":"Talos (%s)"},"conditions":[{"type":"Ready","status":"%s"}]}}]}\n' "$LIVE_TALOS_VERSION" "$ready"
		;;
	esac
else
	exit 1
fi
