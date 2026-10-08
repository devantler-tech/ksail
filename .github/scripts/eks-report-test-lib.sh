#!/usr/bin/env bash

# Shared helpers for the EKS report script tests. Source this file after
# setting reporter (the script under test), fake_bin (the directory holding the
# fake aws), and pass_count=0. Each case sets fake_dir before calling run.

# The variables below are set by the sourcing test or by run, and read by both.
# shellcheck disable=SC2034,SC2154

output=""
status=0

run() {
	status=0
	output="$(FAKE_DIR="${fake_dir}" PATH="${fake_bin}:${PATH}" "${reporter}" "$@" 2>&1)" || status=$?
}

fail() {
	printf 'FAIL: %s\n--- output (status %s) ---\n%s\n---\n' "$1" "${status}" "${output}" >&2
	exit 1
}

expect_status() {
	[[ "${status}" -eq "$1" ]] || fail "$2 (expected status $1)"
}

expect_text() {
	grep -Fq -- "$1" <<<"${output}" || fail "$2"
}

refute_text() {
	if grep -Fq -- "$1" <<<"${output}"; then
		fail "$2"
	fi
}

pass() {
	pass_count=$((pass_count + 1))
	printf 'ok %s\n' "$1"
}

# expect_fenced_group TITLE PREFIX: the output must be one log group titled
# TITLE whose body is fenced off from workflow commands by a token starting
# with PREFIX, closed with that same token.
expect_fenced_group() {
	local title="$1" prefix="$2" fence
	[[ "$(head -n 1 <<<"${output}")" == "::group::${title}" ]] ||
		fail 'the report must open a log group'
	fence="$(sed -n "2s/^::stop-commands::\(${prefix}-[0-9a-f]\{32\}\)\$/\1/p" <<<"${output}")"
	[[ -n "${fence}" ]] || fail 'quoted provider text must be fenced off from workflow commands'
	[[ "$(tail -n 2 <<<"${output}" | head -n 1)" == "::${fence}::" ]] ||
		fail 'the command fence must be closed with its own token'
	[[ "$(tail -n 1 <<<"${output}")" == '::endgroup::' ]] || fail 'the report must close its log group'
}
