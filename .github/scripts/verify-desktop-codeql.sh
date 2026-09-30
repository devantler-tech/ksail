#!/usr/bin/env bash
# Require extracted function bodies from the CLI and every Linux desktop source
# file. A successful upload or source archive alone does not prove this coverage.
set -euo pipefail

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
