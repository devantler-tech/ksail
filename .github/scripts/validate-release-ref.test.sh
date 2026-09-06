#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
validator="$script_dir/validate-release-ref.sh"
checks=0

# expect_accept checks that a valid ref passes the production validator.
expect_accept() {
	local ref=$1 output
	if ! output=$(GITHUB_REF="$ref" bash "$validator" 2>&1); then
		printf 'FAIL: valid release ref %q rejected: %s\n' "$ref" "$output" >&2
		exit 1
	fi
	checks=$((checks + 1))
}

# expect_reject checks rejection and a safe, single-line diagnostic naming the ref.
expect_reject() {
	local ref=$1 output quoted
	if output=$(GITHUB_REF="$ref" bash "$validator" 2>&1); then
		printf 'FAIL: invalid release ref %q accepted\n' "$ref" >&2
		exit 1
	fi
	printf -v quoted '%q' "$ref"
	if [[ $output != *"$quoted"* || $output == *$'\n'* ]]; then
		printf 'FAIL: rejected ref must be reported safely on one line: %q\n' "$output" >&2
		exit 1
	fi
	checks=$((checks + 1))
}

for tag in v7.181.8 v8.0.0-beta.1 v0.0.0 v1.2.3-0 v1.2.3-01a v1.2.3-RC.1 v1.2.3-alpha-beta; do
	expect_accept "refs/tags/$tag"
done

for tag in vanything v/../evil v1latest v01.0.0 v1.01.0 v1.0.01 \
	v1.2 v1.2.3.4 v1.2.3- v1.2.3-01 v1.2.3-beta.01 v1.2.3-alpha..1 \
	v1.2.3+build v1.2.3-alpha_1 V1.2.3 v1.2.3-é; do
	expect_reject "refs/tags/$tag"
done

for ref in '' v1.2.3 refs/heads/v1.2.3 refs/pull/1/merge 'refs/tags/v1.2.3 ' \
	$'refs/tags/v1.2.3\n::error::injected'; do
	expect_reject "$ref"
done

# The OCI limit includes the leading v and prerelease separator.
printf -v suffix '%121s' ''
suffix=${suffix// /a}
expect_accept "refs/tags/v1.2.3-$suffix"
expect_reject "refs/tags/v1.2.3-${suffix}a"

if (
	unset GITHUB_REF
	bash "$validator"
) >/dev/null 2>&1; then
	printf 'FAIL: missing GITHUB_REF accepted\n' >&2
	exit 1
fi

printf 'PASS: release ref validation (%s cases plus missing environment)\n' "$checks"
