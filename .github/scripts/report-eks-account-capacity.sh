#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF_USAGE'
Usage:
  report-eks-account-capacity.sh --region REGION
  report-eks-account-capacity.sh --region REGION --baseline-file NEW_FILE \
    --cluster-name LABEL --run-id ID --run-attempt ATTEMPT --source-sha SHA

Print a read-only regional EKS resource diagnostic.

Lists launch resources, stacks, gateways, addresses, interfaces, volumes and
both load-balancer APIs. Incomplete reads are unknown, never zero. Regional
fleet listing omits instant fleets without their exact IDs. These counts do
not establish run ownership or prove a completed teardown.

It only reads and it creates nothing, so it can be run without a cluster. It
prints validated states, types and counts, never an identifier. Read failures
remain diagnostic and exit 0. Shell option errors exit 2; a runtime/build failure is
nonzero. Requires Go matching go.mod and the AWS CLI.

Baseline mode writes a new owner-only regional identity snapshot instead of the
ordinary report. All context flags are required. Failed reads write unknown
families without partial identities and return nonzero. Existing files and
symlinks are never overwritten. The parent must be an existing Unix directory
owned by the current user with no group or other access (for example, mktemp -d).
The snapshot belongs only to its observation run; new identities do not prove
ownership. Account binding, instant-fleet coverage and teardown remain unknown.
Keep the file private; it is local evidence, not durable cross-job retention.
EOF_USAGE
}

region=""
report_args=()

while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--region)
		if (($# < 2)); then
			usage >&2
			exit 2
		fi
		region="$2"
		shift 2
		;;
	--baseline-file | --cluster-name | --run-id | --run-attempt | --source-sha)
		if (($# < 2)); then
			usage >&2
			exit 2
		fi
		report_args+=("$1" "$2")
		shift 2
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "${region}" =~ ^[a-z]{2}(-[a-z]+)+-[0-9]$ ]]; then
	usage >&2
	exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
printf '::group::EKS regional resources (%s)\n' "${region}"
fence="capacity-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
printf '::stop-commands::%s\n' "${fence}"
trap 'printf "::%s::\n::endgroup::\n" "${fence}"' EXIT
cd "${repo_root}"
report_args+=(--region "${region}")
go run ./internal/maintenance/eksinventory/cmd "${report_args[@]}"
