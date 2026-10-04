#!/usr/bin/env bash
# Reject failed Go project extraction and require function bodies from the CLI
# and every Linux desktop source file, the owned logging adapter, and authenticated dependency modules. An
# upload alone does not prove coverage.
set -euo pipefail

# verify_results rejects incomplete or malformed extraction evidence.
verify_results() {
	jq -e '
    .["#select"].tuples as $rows |
    ($rows | type == "array") and
    ($rows | length == 25) and
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
      ["desktop/window_state.go", "trackWindowState"],
      ["third_party/cel-go/cel/env.go", "NewEnv"],
      ["third_party/glamour/glamour.go", "NewTermRenderer"],
      ["third_party/go-macholibre/universal_binary.go", "ExtractReaders"],
      ["third_party/kyverno-jmespath/api.go", "Search"],
      ["third_party/jmespath/api.go", "Search"],
      ["third_party/ansi/width.go", "Strip"],
      ["third_party/ansi-runtime/width.go", "Strip"],
      ["third_party/redisotel/tracing.go", "InstrumentTracing"],
      ["third_party/rediscmd/rediscmd.go", "CmdString"],
      ["third_party/dynamiclistener/cert/cert.go", "NewPrivateKey"],
      ["third_party/dynamiclistener/factory/cert_utils.go", "ParseCertPEM"],
      ["third_party/otelzap/otelzap.go", "log"],
      ["third_party/otelzap/logvalue.go", "logValue"],
      ["internal/codeqlprofile/metrics.go", "ParseTime"],
      ["internal/codeqlprofile/metrics.go", "Summarize"],
      ["internal/codeqlprofile/files.go", "ReadMeasurements"],
      ["internal/codeqlprofile/cmd/main.go", "main"]
    ] | sort)
  ' "$1" >/dev/null || {
		printf '::error::CodeQL database lacks required CLI, desktop, adapter, or dependency function bodies.\n' >&2
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
		# Legacy workflow commands can match anywhere in a log line. Unicode-escape
		# hashes after JSON encoding so diagnostic text remains data in both parsers.
		jq -r '.[] | select(.source.id == "go/autobuilder/extraction-failed-for-project") |
      {source: .source.id, message: (.plaintextMessage // .source.name)} |
      tojson | gsub("#"; "\\u0023")' "$1"
		printf '::error::CodeQL failed to extract a Go project; the database is incomplete.\n' >&2
		return 1
	fi
}

if [[ $# == 2 && "$1" == --results ]]; then
	verify_results "$2"
	exit
fi
diagnostics_only=false
if [[ $# == 2 && "$1" == --diagnostics-only ]]; then
	diagnostics_only=true
	shift
elif [[ $# != 1 ]]; then
	printf 'Usage: %s [--diagnostics-only] DATABASE | --results BQRS_JSON\n' "$0" >&2
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

# Go autobuild can complete successfully after a module's extractor failed.
# Export all diagnostics, including warnings hidden from the uploaded results.
if ! "${CODEQL_CLI}" database export-diagnostics --format=raw -- "$1" >"${coverage_dir}/diagnostics.json"; then
	printf '::error::CodeQL extraction diagnostics could not be exported.\n' >&2
	exit 1
fi
verify_diagnostics "${coverage_dir}/diagnostics.json"
if [[ "${diagnostics_only}" == true ]]; then exit; fi
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
