#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
cleanup="${script_dir}/delete-old-workflow-runs.sh"
fake_bin="${repo_root}/.github/fixtures/workflow-run-cleanup/fake-bin"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

state_dir="${tmp_dir}/bounded"
mkdir -p "${state_dir}"

PATH="${fake_bin}:${PATH}" \
	GH_TOKEN=fixture \
	FAKE_GH_STATE_DIR="${state_dir}" \
	KSAIL_MAINTENANCE_CUTOFF_DATE=2026-07-02 \
	"${cleanup}" \
	--repository devantler-tech/ksail \
	--retain-days 30 \
	--keep-minimum-runs 2 \
	--max-deletions 2 >"${tmp_dir}/output"

printf '1005\n1006\n' >"${tmp_dir}/expected-deletions"
diff -u "${tmp_dir}/expected-deletions" "${state_dir}/deleted"
grep -Fq 'Deletion limit reached: 2' "${tmp_dir}/output"
printf 'PASS: cleanup preserves protected runs and stops at the deletion limit\n'

failure_state="${tmp_dir}/api-failure"
mkdir -p "${failure_state}"
if PATH="${fake_bin}:${PATH}" \
	GH_TOKEN=fixture \
	FAKE_GH_SCENARIO=api-failure \
	FAKE_GH_STATE_DIR="${failure_state}" \
	KSAIL_MAINTENANCE_CUTOFF_DATE=2026-07-02 \
	"${cleanup}" \
	--repository devantler-tech/ksail \
	--retain-days 30 \
	--keep-minimum-runs 2 \
	--max-deletions 2 >"${tmp_dir}/failure-output" 2>&1; then
	printf 'FAIL: cleanup accepted a workflow API failure\n' >&2
	exit 1
fi
if [[ -e "${failure_state}/deleted" ]]; then
	printf 'FAIL: cleanup deleted runs after an API failure\n' >&2
	exit 1
fi
printf 'PASS: cleanup fails closed before deletion on an API error\n'

delete_failure_state="${tmp_dir}/delete-failure"
mkdir -p "${delete_failure_state}"
if PATH="${fake_bin}:${PATH}" \
	GH_TOKEN=fixture \
	FAKE_GH_SCENARIO=delete-failure \
	FAKE_GH_STATE_DIR="${delete_failure_state}" \
	KSAIL_MAINTENANCE_CUTOFF_DATE=2026-07-02 \
	"${cleanup}" \
	--repository devantler-tech/ksail \
	--retain-days 30 \
	--keep-minimum-runs 2 \
	--max-deletions 3 >"${tmp_dir}/delete-failure-output" 2>&1; then
	printf 'FAIL: cleanup hid a workflow-run deletion failure\n' >&2
	exit 1
fi
printf '1006\n1007\n' >"${tmp_dir}/expected-delete-failure-deletions"
diff -u "${tmp_dir}/expected-delete-failure-deletions" "${delete_failure_state}/deleted"
grep -Fq 'ERROR: failed to delete workflow run 1005 (101)' "${tmp_dir}/delete-failure-output"
grep -Fq 'Cleanup completed with 1 failed deletion attempt(s)' "${tmp_dir}/delete-failure-output"
printf 'PASS: cleanup continues its bounded batch and reports deletion failures\n'
