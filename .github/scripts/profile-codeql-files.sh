#!/usr/bin/env bash
# Export only normalized counts and the canonical repository Go-file set hash.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

summarize_files() {
	local input="$1" hash
	jq -se '
    length == 1 and (.[0]["#select"].tuples as $rows |
      ($rows|type == "array" and length > 0) and
      all($rows[]; type == "array" and length == 1 and
        (.[0]|type == "string" and endswith(".go") and
          (startswith("/")|not) and (contains("\\")|not) and
          (contains("//")|not) and (test("(^|/)\\.\\.?(/|$)")|not) and
          (explode|all(. >= 32 and . != 127)))) and
      (($rows|map(.[0])|unique|length) == ($rows|length)))
  ' "$input" >/dev/null
	jq -r '.["#select"].tuples[][0]' "$input" | LC_ALL=C sort >"$scratch/files"
	if command -v sha256sum >/dev/null; then
		hash="$(sha256sum "$scratch/files" | cut -d ' ' -f 1)"
	else
		hash="$(shasum -a 256 "$scratch/files" | cut -d ' ' -f 1)"
	fi
	[[ "$hash" =~ ^[0-9a-f]{64}$ ]]
	jq --arg hash "$hash" '{sourceGoFiles:(.["#select"].tuples|length),
    desktopGoFiles:([.["#select"].tuples[][0]|select(startswith("desktop/"))]|length),
    sourceFilesSha256:$hash}' "$input"
}

if [[ $# == 2 && "$1" == --results ]]; then
	summarize_files "$2"
	exit
fi
[[ $# == 1 ]]
: "${CODEQL_CLI:?A CodeQL CLI is required}"
cp "$script_dir/../codeql/desktop-coverage/"* "$scratch/"
search_path="$("$CODEQL_CLI" resolve qlpacks --format=json | jq -er '[.[][]] | join(":")')"
[[ -n "$search_path" ]]
"$CODEQL_CLI" pack install --search-path="$search_path" --common-caches="$scratch/cache" "$scratch" >&2
"$CODEQL_CLI" query run --database="$1" --search-path="$search_path" \
	--common-caches="$scratch/cache" --threads=1 --ram=2048 \
	--output="$scratch/files.bqrs" "$scratch/files.ql" >&2
"$CODEQL_CLI" bqrs decode --format=json --output="$scratch/files.json" "$scratch/files.bqrs" >&2
summarize_files "$scratch/files.json"
