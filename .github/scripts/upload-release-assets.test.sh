#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
uploader="${script_dir}/upload-release-assets.sh"
cd_workflow="${repo_root}/.github/workflows/cd.yaml"
ci_workflow="${repo_root}/.github/workflows/ci.yaml"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
fake_bin="${tmp_dir}/fake-bin"
assets_dir="${tmp_dir}/dist-assets"
pass_count=0

mkdir -p "${fake_bin}" "${assets_dir}"
printf 'vsix\n' >"${assets_dir}/ksail-7.175.1.vsix"
printf 'zip\n' >"${assets_dir}/KSail_7.175.1_darwin_arm64.zip"

# The fake gh fails its first FAKE_GH_FAILURES calls with the HTTP 503 uploads.github.com returned when
# this broke a release, then succeeds. Every call's arguments are logged, one call per line.
cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_GH_LOG}"
calls="$(wc -l <"${FAKE_GH_LOG}")"
if ((calls <= FAKE_GH_FAILURES)); then
	printf 'HTTP 503: Service Unavailable (https://uploads.github.com/)\n' >&2
	exit 1
fi
EOF
# The fake sleep records each delay instead of waiting, so the backoff is asserted without slowing the suite.
cat >"${fake_bin}/sleep" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$1" >>"${FAKE_SLEEP_LOG}"
EOF
chmod +x "${fake_bin}/gh" "${fake_bin}/sleep"

# run_case runs the uploader with a fake gh failing FAILURES times and checks its exit status, how many
# uploads it attempted, and that its combined output contains EXPECTED (or lacks it when prefixed by !).
run_case() {
	local name="$1" failures="$2" want_status="$3" want_calls="$4" expected="$5"
	shift 5
	local case_dir="${tmp_dir}/${name}"
	mkdir -p "${case_dir}"
	: >"${case_dir}/gh.log"
	: >"${case_dir}/sleep.log"
	local status=0
	# LC_ALL=C pins the glob order the upload-call assertion below expects; locales that fold case sort
	# ksail-7… before KSail_7….
	LC_ALL=C PATH="${fake_bin}:${PATH}" FAKE_GH_FAILURES="${failures}" FAKE_GH_LOG="${case_dir}/gh.log" \
		FAKE_SLEEP_LOG="${case_dir}/sleep.log" GH_REPO=devantler-tech/ksail \
		"${uploader_under_test:-${uploader}}" "$@" >"${case_dir}/output" 2>&1 || status=$?
	local calls
	calls="$(wc -l <"${case_dir}/gh.log" | tr -d ' ')"
	if [[ "${status}" -ne "${want_status}" || "${calls}" -ne "${want_calls}" ]]; then
		printf 'FAIL: %s: status %s (want %s), uploads %s (want %s)\n' \
			"${name}" "${status}" "${want_status}" "${calls}" "${want_calls}" >&2
		sed 's/^/  | /' "${case_dir}/output" >&2
		exit 1
	fi
	if [[ "${expected}" == !* ]]; then
		if grep -qF -- "${expected#!}" "${case_dir}/output"; then
			printf 'FAIL: %s: output must not contain %s\n' "${name}" "${expected#!}" >&2
			sed 's/^/  | /' "${case_dir}/output" >&2
			exit 1
		fi
	elif ! grep -qF -- "${expected}" "${case_dir}/output"; then
		printf 'FAIL: %s: output lacks %s\n' "${name}" "${expected}" >&2
		sed 's/^/  | /' "${case_dir}/output" >&2
		exit 1
	fi
	pass_count=$((pass_count + 1))
	printf 'PASS: %s\n' "${name}"
}

tag_args=(--tag v7.175.1 --assets-dir "${assets_dir}")

run_case first-attempt-uploads-quietly 0 0 1 '!on attempt' "${tag_args[@]}"
# Every downloaded file goes up in one call, to the release's repository, replacing partial uploads.
upload_call="$(cat "${tmp_dir}/first-attempt-uploads-quietly/gh.log")"
want_call="release upload v7.175.1 ${assets_dir}/KSail_7.175.1_darwin_arm64.zip ${assets_dir}/ksail-7.175.1.vsix --repo devantler-tech/ksail --clobber"
if [[ "${upload_call}" != "${want_call}" ]]; then
	printf 'FAIL: upload call was\n  %s\nwant\n  %s\n' "${upload_call}" "${want_call}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: uploads-every-asset-with-clobber\n'

run_case transient-failures-recover 2 0 3 'uploaded 2 asset(s) to v7.175.1 on attempt 3/5' "${tag_args[@]}"

run_case persistent-failure-leaves-draft 99 1 5 \
	'Nothing was published; v7.175.1 is still a draft, which the cleanup job removes.' "${tag_args[@]}" --delay-seconds 1
sleeps="$(paste -sd' ' "${tmp_dir}/persistent-failure-leaves-draft/sleep.log")"
if [[ "${sleeps}" != "1 2 4 8" ]]; then
	printf 'FAIL: backoff must double between attempts and not sleep after the last (slept: %s)\n' "${sleeps}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: backoff-doubles-between-attempts\n'

run_case missing-assets-dir-fails-fast 99 1 0 'does not exist' --tag v7.175.1 --assets-dir "${tmp_dir}/absent"
mkdir -p "${tmp_dir}/only-dirs/nested"
run_case empty-assets-dir-fails-fast 99 1 0 'holds no files' --tag v7.175.1 --assets-dir "${tmp_dir}/only-dirs"
run_case zero-attempts-rejected 0 2 0 '--attempts must be' "${tag_args[@]}" --attempts 0
run_case missing-tag-rejected 0 2 0 '--tag, --assets-dir' --assets-dir "${assets_dir}"

# The single unretried call this replaces strands the release on the same transient the uploader recovers
# from: prove the old form fails where the new one passes, so the retry is what makes the difference.
old_form="${tmp_dir}/old-form.sh"
cat >"${old_form}" <<EOF
#!/usr/bin/env bash
set -euo pipefail
cd "${tmp_dir}"
gh release upload "\$2" dist-assets/* --clobber
EOF
chmod +x "${old_form}"
uploader_under_test="${old_form}" run_case old-single-call-strands-on-one-503 1 1 1 'HTTP 503' "${tag_args[@]}"
run_case retrying-upload-survives-one-503 1 0 2 'on attempt 2/5' "${tag_args[@]}"

publish_block="${tmp_dir}/publish-release.yaml"
awk '/^  publish-release:/ { inside = 1; print; next } inside && /^  [a-z]/ { exit } inside { print }' \
	"${cd_workflow}" >"${publish_block}"
[[ -s "${publish_block}" ]] || {
	printf 'FAIL: publish-release job not found in cd.yaml\n' >&2
	exit 1
}

# cd.yaml must attach assets through the retrying uploader and never with a bare, unretried upload.
# shellcheck disable=SC2016 # $RELEASE_TAG is matched literally, as it appears in the workflow.
if ! grep -qF -- '.github/scripts/upload-release-assets.sh --tag "$RELEASE_TAG" --assets-dir dist-assets' \
	"${publish_block}"; then
	printf 'FAIL: publish-release must attach assets with .github/scripts/upload-release-assets.sh\n' >&2
	exit 1
fi
bare="$(grep -cF -- 'gh release upload' "${cd_workflow}" || true)"
if [[ "${bare}" -ne 0 ]]; then
	printf 'FAIL: cd.yaml still calls gh release upload directly %s time(s)\n' "${bare}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: workflow-attaches-with-retrying-upload\n'

# The job timeout must leave room for the whole backoff on top of the rest of the job, or the runner
# cancels it mid-retry and the final "still a draft" message is never written.
attempts="$(sed -n 's/^DEFAULT_ATTEMPTS=\([0-9][0-9]*\)$/\1/p' "${uploader}")"
delay="$(sed -n 's/^DEFAULT_DELAY_SECONDS=\([0-9][0-9]*\)$/\1/p' "${uploader}")"
timeout_minutes="$(sed -n 's/^    timeout-minutes: \([0-9][0-9]*\)$/\1/p' "${publish_block}" | head -n 1)"
if [[ -z "${attempts}" || -z "${delay}" || -z "${timeout_minutes}" ]]; then
	printf 'FAIL: could not read the retry defaults (%s, %s) or the job timeout (%s)\n' \
		"${attempts}" "${delay}" "${timeout_minutes}" >&2
	exit 1
fi
backoff=0
step="${delay}"
for ((i = 1; i < attempts; i++)); do
	backoff=$((backoff + step))
	step=$((step * 2))
done
# 300s is the budget the job ran within before retries existed.
if ((timeout_minutes * 60 < backoff + 300)); then
	printf 'FAIL: publish-release timeout %sm cannot fit %ss of backoff plus the 300s the job already needs\n' \
		"${timeout_minutes}" "${backoff}" >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: workflow-timeout-fits-retry-budget\n'

# CI must execute this suite, not only lint it.
executed="$(grep -cxE '[[:space:]]*\.github/scripts/upload-release-assets\.test\.sh' "${ci_workflow}" || true)"
if [[ "${executed}" -lt 1 ]]; then
	printf 'FAIL: ci.yaml must execute .github/scripts/upload-release-assets.test.sh\n' >&2
	exit 1
fi
pass_count=$((pass_count + 1))
printf 'PASS: ci-executes-upload-release-assets-suite\n'

printf 'All %d release asset upload cases passed.\n' "${pass_count}"
