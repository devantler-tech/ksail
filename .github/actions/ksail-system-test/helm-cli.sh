#!/usr/bin/env bash
# Sourced by the system-test trials that drive the Helm CLI against a release KSail
# installed. The caller defines fail() and passes the release namespace and its log
# directory; this records the Helm version there and sets the arguments that differ
# between Helm 3 and Helm 4.
# shellcheck disable=SC2034 # the arrays and flag are read by the sourcing script.

select_helm_cli_args() {
	local namespace="$1" log_dir="$2"
	command -v helm >/dev/null || fail 'Helm CLI is required to seed the live release'
	helm version --short >"$log_dir/helm-version.txt"
	case "$(cat "$log_dir/helm-version.txt")" in
	v3.*)
		helm_list_args=(--all --namespace "$namespace")
		helm_rollback_flag=--atomic
		helm_conflict_args=()
		;;
	v4.*)
		# Helm 4 lists all states by default and renamed the rollback option.
		helm_list_args=(--namespace "$namespace")
		helm_rollback_flag=--rollback-on-failure
		# KSail installs through the Helm 4 SDK with server-side apply under its own field
		# manager, so an out-of-band CLI edit must take ownership of the fields it seeds.
		helm_conflict_args=(--server-side=true --force-conflicts)
		;;
	*) fail 'unsupported Helm major version' ;;
	esac
}
