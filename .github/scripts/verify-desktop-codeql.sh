#!/usr/bin/env bash
# Reject failed Go project extraction and require function bodies from the CLI
# and every Linux desktop source file. An upload alone does not prove coverage.
set -euo pipefail

# verify_results rejects incomplete or malformed CLI/desktop extraction evidence.
verify_results() {
	jq -e '
    .["#select"].tuples as $rows |
    ($rows | type == "array") and
    ($rows | length == 8) and
    ($rows | all(length == 3 and
      (.[2] | type == "number" and . > 0 and floor == .))) and
    ([$rows[] | .[0:2]] | sort) == ([
      ["main.go", "main"],
      ["desktop/main.go", "main"],
      ["desktop/main.go", "run"],
      ["desktop/env_other.go", "hydrateLoginShellEnv"],
      ["desktop/menu.go", "installApplicationMenu"],
      ["desktop/deeplink.go", "handleDeepLink"],
      ["desktop/notify.go", "watchClusterStatus"],
      ["desktop/window_state.go", "trackWindowState"]
    ] | sort)
  ' "$1" >/dev/null || {
		printf '::error::CodeQL database lacks the required CLI/desktop function bodies.\n' >&2
		return 1
	}
}

# verify_diagnostics rejects module failures even when sampled bodies survived.
verify_diagnostics() {
	jq -se '
    length == 1 and (.[0] | type == "array" and all(.[];
      .source.id | type == "string" and length > 0))
  ' "$1" >/dev/null || {
		printf '::error::CodeQL extraction diagnostics are malformed.\n' >&2
		return 1
	}
	if jq -e 'any(.[]; .source.id == "go/autobuilder/extraction-failed-for-project")' "$1" >/dev/null; then
		jq -c '.[] | select(.source.id == "go/autobuilder/extraction-failed-for-project") |
      {source: .source.id, message: (.plaintextMessage // .source.name)}' "$1"
		printf '::error::CodeQL failed to extract a Go project; the database is incomplete.\n' >&2
		return 1
	fi
}

if [[ $# == 2 && "$1" == --results ]]; then
	verify_results "$2"
	exit
fi
if [[ $# != 1 ]]; then
	printf 'Usage: %s DATABASE | --results BQRS_JSON\n' "$0" >&2
	exit 2
fi

: "${CODEQL_CLI:?CodeQL init must provide an executable CLI path}"
[[ -x "${CODEQL_CLI}" ]] || {
	printf '::error::CodeQL CLI is unavailable.\n' >&2
	exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
coverage_dir="$(mktemp -d)"
trap 'rm -rf "${coverage_dir}"' EXIT
cp "${script_dir}/../codeql/desktop-coverage/"* "${coverage_dir}/"

# Go autobuild can complete successfully after a module's extractor failed.
# Export all diagnostics, including warnings hidden from the uploaded results.
if ! "${CODEQL_CLI}" database export-diagnostics --format=raw -- "$1" >"${coverage_dir}/diagnostics.json"; then
	printf '::error::CodeQL extraction diagnostics could not be exported.\n' >&2
	exit 1
fi
verify_diagnostics "${coverage_dir}/diagnostics.json"

# Resolve the library shipped with this analysis bundle, rather than independently
# selecting a newer query library. Keep generated pack locks/cache outside the repo.
pack_search_path="$("${CODEQL_CLI}" resolve qlpacks --format=json | jq -er '[.[][]] | join(":")')"
[[ -n "${pack_search_path}" ]]
"${CODEQL_CLI}" pack install --search-path="${pack_search_path}" \
	--common-caches="${coverage_dir}/cache" "${coverage_dir}"
"${CODEQL_CLI}" query run --database="$1" --search-path="${pack_search_path}" \
	--common-caches="${coverage_dir}/cache" --threads=1 --ram=2048 \
	--output="${coverage_dir}/coverage.bqrs" "${coverage_dir}/coverage.ql"
"${CODEQL_CLI}" bqrs decode --format=json --output="${coverage_dir}/coverage.json" \
	"${coverage_dir}/coverage.bqrs"
cat "${coverage_dir}/coverage.json"
verify_results "${coverage_dir}/coverage.json"
