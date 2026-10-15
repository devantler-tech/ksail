#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF_USAGE'
Usage: check-eks-capacity.sh --region REGION --cluster-name NAME --peak-nodes N < eks.json

Read the complete generated eksctl configuration as JSON on standard input.
Only a supported smoke plan with completely observed regional quota headroom
and a peak within five default vCPUs is admitted. Refusal or unknown exits
nonzero. This command only reads AWS and creates nothing. Admission does not
certify cleanup, reserve capacity or authorize a billable trial.
Requires Go matching go.mod and an authenticated, current AWS CLI.
EOF_USAGE
}

region=""
cluster=""
peak=""
while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--region | --cluster-name | --peak-nodes)
		if (($# < 2)); then
			usage >&2
			exit 2
		fi
		case "$1" in
		--region) region="$2" ;;
		--cluster-name) cluster="$2" ;;
		--peak-nodes) peak="$2" ;;
		esac
		shift 2
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
done
if [[ ! "$region" =~ ^[a-z]{2}(-[a-z]+)+-[0-9]+$ || ! "$cluster" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ || ! "$peak" =~ ^[1-9][0-9]?$ ]]; then
	usage >&2
	exit 2
fi
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
fence="capacity-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
printf '::stop-commands::%s\n' "$fence"
trap 'printf "::%s::\n" "$fence"' EXIT
cd "$repo_root"
go run ./internal/maintenance/eksinventory/capacitycmd --region "$region" --cluster-name "$cluster" --peak-nodes "$peak"
