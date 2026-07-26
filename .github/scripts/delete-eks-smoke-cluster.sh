#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  delete-eks-smoke-cluster.sh --cluster-name NAME --region REGION
                              --workdir DIR --create-attempted true|false

Tear down the ephemeral EKS smoke-test cluster and confirm it is gone.

Success is judged by a post-condition — that no cluster of that name remains —
not by the exit status of any single delete command. A create that failed before
the cluster existed has nothing to tear down and reports success, which keeps a
failed teardown meaning the one thing worth acting on: a billable cluster may
still be running.

Absence must be proven by an explicit not-found response. Any other probe error
leaves the cluster state unknown and fails closed, because reporting a clean
teardown that was never confirmed is what strands a billable cluster silently.
EOF
}

cluster_name=""
region=""
workdir=""
create_attempted=""

while (($# > 0)); do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--cluster-name)
		cluster_name="${2:-}"
		shift 2
		;;
	--region)
		region="${2:-}"
		shift 2
		;;
	--workdir)
		workdir="${2:-}"
		shift 2
		;;
	--create-attempted)
		create_attempted="${2:-}"
		shift 2
		;;
	*)
		printf 'Unknown argument: %s\n\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ -z "${cluster_name}" || -z "${region}" || -z "${workdir}" ]]; then
	printf '--cluster-name, --region and --workdir are required.\n\n' >&2
	usage >&2
	exit 2
fi

# Returns 0 only when the cluster is gone or is irreversibly on its way out.
cluster_absent() {
	local output
	local status=0

	output="$(aws eks describe-cluster \
		--name "${cluster_name}" \
		--region "${region}" \
		--query 'cluster.status' \
		--output text 2>&1)" || status=$?

	if ((status == 0)); then
		# Teardown is asynchronous, so a cluster still reporting DELETING is a
		# delete that took effect rather than a leak. Treating it as one would
		# just move the false alarm from failed creates to successful deletes.
		if [[ "${output}" == "DELETING" ]]; then
			printf 'Cluster %s is already tearing down in %s.\n' "${cluster_name}" "${region}"
			return 0
		fi

		return 1
	fi

	case "${output}" in
	*ResourceNotFoundException*)
		return 0
		;;
	*)
		printf '::warning::Could not determine whether cluster %s still exists in %s: %s\n' \
			"${cluster_name}" "${region}" "${output}" >&2
		return 1
		;;
	esac
}

# Anything other than an explicit false is treated as "a cluster may exist".
# Silently skipping teardown on a typo is the one failure this script exists to
# prevent, so an unrecognised value is a usage error rather than a skip.
if [[ "${create_attempted}" != "true" && "${create_attempted}" != "false" ]]; then
	printf -- '--create-attempted must be exactly true or false (got %s).\n\n' \
		"${create_attempted:-<empty>}" >&2
	usage >&2
	exit 2
fi

if [[ "${create_attempted}" == "false" ]]; then
	echo "Cluster creation did not start; nothing to clean up."
	exit 0
fi

# ksail needs the scaffolded project directory; eksctl addresses the cluster by
# name and region. A missing workdir therefore removes one teardown path, never
# the obligation to verify that nothing is left running.
if [[ -d "${workdir}" ]]; then
	cd "${workdir}"
	ksail cluster delete --provider AWS --name "${cluster_name}" --force ||
		echo "::warning::ksail cluster delete failed."
else
	echo "::warning::No EKS smoke workdir at ${workdir}; skipping ksail and using eksctl."
fi

# A zero exit from ksail does not prove the cluster is gone, so the fallback is
# driven by what AWS reports rather than by the previous command's status —
# otherwise a delete that silently no-ops spends only one of the two attempts.
if ! cluster_absent; then
	echo "::warning::Cluster still present; falling back to eksctl delete cluster."
	eksctl delete cluster \
		--name "${cluster_name}" \
		--region "${region}" \
		--wait || true
fi

if cluster_absent; then
	printf 'No cluster %s remains in %s.\n' "${cluster_name}" "${region}"
	exit 0
fi

printf '::error::EKS cleanup did not complete. Cluster %s is still present in %s and may be billable.\n' \
	"${cluster_name}" "${region}" >&2
exit 1
