#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
checkpoint="${script_dir}/redraft-evergreen-cask-prs.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"

cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

scenario="${CHECKPOINT_SCENARIO:?}"
state_dir="${CHECKPOINT_STATE_DIR:?}"
command_line="$*"
old_sha="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
new_sha="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

if [[ "${command_line}" == "api repos/devantler-tech/homebrew-tap/pulls?state=open&head=devantler-tech:goreleaser/ksail&per_page=100" ]]; then
	if [[ "${scenario}" == open ]]; then
		printf '%s\n' '[{"number":77,"draft":true,"node_id":"PR_77","user":{"login":"devantler"},"head":{"ref":"goreleaser/ksail","repo":{"full_name":"devantler-tech/homebrew-tap"}}}]'
	elif [[ "${scenario}" == untrusted-open ]]; then
		printf '%s\n' '[{"number":78,"draft":false,"node_id":"PR_78","user":{"login":"another-author"},"head":{"ref":"goreleaser/ksail","repo":{"full_name":"devantler-tech/homebrew-tap"}}}]'
	elif [[ "${scenario}" == pr-appeared && -f "${state_dir}/ref-read-final" ]]; then
		printf '%s\n' '[{"number":79,"draft":true,"node_id":"PR_79","user":{"login":"another-author"},"head":{"ref":"goreleaser/ksail","repo":{"full_name":"devantler-tech/homebrew-tap"}}}]'
	else
		printf '%s\n' '[]'
	fi
elif [[ "${command_line}" == "api repos/devantler-tech/homebrew-tap/git/matching-refs/heads/goreleaser/ksail" ]]; then
	if [[ "${scenario}" == no-branch ]]; then
		printf '%s\n' '[]'
	else
		if [[ -f "${state_dir}/ref-read" ]]; then
			touch "${state_dir}/ref-read-final"
			if [[ "${scenario}" == moved-ref ]]; then
				printf '[{"ref":"refs/heads/goreleaser/ksail","object":{"sha":"%s"}}]\n' "${new_sha}"
			else
				printf '[{"ref":"refs/heads/goreleaser/ksail","object":{"sha":"%s"}}]\n' "${old_sha}"
			fi
		else
			touch "${state_dir}/ref-read"
			printf '[{"ref":"refs/heads/goreleaser/ksail","object":{"sha":"%s"}}]\n' "${old_sha}"
		fi
	fi
elif [[ "${command_line}" == "api --paginate --slurp repos/devantler-tech/homebrew-tap/pulls?state=closed&head=devantler-tech:goreleaser/ksail&per_page=100" ]]; then
	evidence_sha="${old_sha}"
	[[ "${scenario}" == no-evidence ]] && evidence_sha="${new_sha}"
	printf '[[{"number":42,"state":"closed","merged_at":"2026-07-19T00:00:00Z","user":{"login":"devantler"},"head":{"sha":"%s","ref":"goreleaser/ksail","repo":{"full_name":"devantler-tech/homebrew-tap"}}}]]\n' "${evidence_sha}"
else
	printf 'unexpected fake gh invocation: %s\n' "${command_line}" >&2
	exit 2
fi
EOF

cat >"${fake_bin}/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"${CHECKPOINT_STATE_DIR:?}/git-call"
[[ "${CHECKPOINT_SCENARIO:?}" == delete-failure ]] && exit 1
exit 0
EOF
chmod +x "${fake_bin}/gh" "${fake_bin}/git"

pass_count=0
run_case() {
	local scenario="$1" expected_status="$2" expected_output="$3" expected_delete="$4"
	local case_dir="${tmp_dir}/${scenario}" output status
	mkdir -p "${case_dir}"
	set +e
	output="$(PATH="${fake_bin}:${PATH}" \
		GH_TOKEN=test-token \
		CHECKPOINT_SCENARIO="${scenario}" \
		CHECKPOINT_STATE_DIR="${case_dir}" \
		"${checkpoint}" --tap devantler-tech/homebrew-tap ksail 2>&1)"
	status=$?
	set -e

	if [[ "${status}" -ne "${expected_status}" ]]; then
		printf 'FAIL: %s: expected status %s, got %s\n%s\n' \
			"${scenario}" "${expected_status}" "${status}" "${output}" >&2
		return 1
	fi
	if [[ "${output}" != *"${expected_output}"* ]]; then
		printf 'FAIL: %s: expected output containing %q, got:\n%s\n' \
			"${scenario}" "${expected_output}" "${output}" >&2
		return 1
	fi
	if [[ "${expected_delete}" == true ]]; then
		if [[ ! -s "${case_dir}/git-call" ]] ||
			! grep -Fq -- '--force-with-lease=refs/heads/goreleaser/ksail:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
				"${case_dir}/git-call"; then
			printf 'FAIL: %s: expected an exact-SHA leased branch delete\n' "${scenario}" >&2
			return 1
		fi
	elif [[ -e "${case_dir}/git-call" ]]; then
		printf 'FAIL: %s: branch deletion must not be attempted\n' "${scenario}" >&2
		return 1
	fi

	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${scenario}"
}

run_case no-branch 0 'no retained branch' false
run_case open 0 'already a draft' false
run_case untrusted-open 1 'not trusted for re-draft' false
run_case terminal 0 'Removed terminal branch' true
run_case pr-appeared 1 'acquired an open PR' false
run_case moved-ref 1 'moved before deletion' false
run_case delete-failure 1 'could not delete terminal branch' true
run_case no-evidence 1 'no terminal PR evidence matching' false

printf 'All %d evergreen cask checkpoint cases passed.\n' "${pass_count}"
