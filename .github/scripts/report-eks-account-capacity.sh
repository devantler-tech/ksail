#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF_USAGE'
Usage:
  report-eks-account-capacity.sh --region REGION

Print a read-only regional EKS resource diagnostic.

Lists launch resources, stacks, gateways, addresses, interfaces, volumes and
both load-balancer APIs. Incomplete reads are unknown, never zero. Regional
fleet listing omits instant fleets without their exact IDs. These counts do
not establish run ownership or prove a completed teardown.

It only reads and it creates nothing, so it can be run without a cluster. It
prints validated states, types and counts, never an identifier. Read failures
remain diagnostic and exit 0. Usage errors exit 2; a runtime/build failure is
nonzero. Requires Go matching go.mod and the AWS CLI.
EOF_USAGE
}

region=""

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
go run ./internal/maintenance/eksinventory/cmd --region "${region}"
