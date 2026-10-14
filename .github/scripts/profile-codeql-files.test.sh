#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
printf '%s\n' '{"#select":{"tuples":[["pkg/file.go"],["desktop/main.go"]]}}' >"$scratch/valid.json"
result="$(bash "$script_dir/profile-codeql-files.sh" --results "$scratch/valid.json")"
jq -e '.sourceGoFiles == 2 and .desktopGoFiles == 1 and (.sourceFilesSha256|test("^[0-9a-f]{64}$"))' <<<"$result" >/dev/null
printf '%s\n' '{"#select":{"tuples":[["desktop/main.go"],["pkg/file.go"]]}}' >"$scratch/reordered.json"
[[ "$(bash "$script_dir/profile-codeql-files.sh" --results "$scratch/reordered.json")" == "$result" ]]
printf '%s\n' '{"#select":{"tuples":[["pkg/other.go"],["desktop/main.go"]]}}' >"$scratch/different.json"
other="$(bash "$script_dir/profile-codeql-files.sh" --results "$scratch/different.json")"
[[ "$(jq -r .sourceFilesSha256 <<<"$other")" != "$(jq -r .sourceFilesSha256 <<<"$result")" ]]
for invalid in '{"#select":{"tuples":[]}}' \
	'{"#select":{"tuples":[["file.go"],["file.go"]]}}' \
	'{"#select":{"tuples":[["../escape.go"]]}}' \
	'{"#select":{"tuples":[["/absolute.go"]]}}' \
	'{"#select":{"tuples":[["pkg/../escape.go"]]}}' \
	'{"#select":{"tuples":[["bad\nfile.go"]]}}' \
	'{"#select":{"tuples":[["file.go",1]]}}' \
	'{"#select":{"tuples":[["go.mod"]]}}' \
	'{"#select":{"tuples":[[null]]}}' \
	'{"#select":{"tuples":[["file.go"]]}} {}'; do
	printf '%s\n' "$invalid" >"$scratch/invalid.json"
	if bash "$script_dir/profile-codeql-files.sh" --results "$scratch/invalid.json" >"$scratch/output" 2>&1; then
		printf 'FAIL: malformed native file result accepted\n' >&2
		exit 1
	fi
done
printf 'PASS: native file-set count/hash, ordering, unequal sets and malformed results\n'
