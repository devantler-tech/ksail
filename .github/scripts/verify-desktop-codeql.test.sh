#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
validator="${script_dir}/verify-desktop-codeql.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT

cat >"${scratch}/positive.json" <<'JSON'
{"#select":{"tuples":[
["main.go","main",1],
["desktop/main.go","main",1],
["desktop/main.go","run",1],
["desktop/env_other.go","hydrateLoginShellEnv",1],
["desktop/menu.go","installApplicationMenu",1],
["desktop/deeplink.go","handleDeepLink",1],
["desktop/notify.go","watchClusterStatus",1],
["desktop/window_state.go","trackWindowState",1]
]}}
JSON

bash "${validator}" --results "${scratch}/positive.json" >/dev/null

# reject requires the named negative fixture to fail the extraction validator.
reject() {
	local name="$1"
	if bash "${validator}" --results "${scratch}/${name}.json" >/dev/null 2>&1; then
		printf 'FAIL: accepted %s extraction evidence\n' "${name}" >&2
		exit 1
	fi
}

for index in {0..7}; do
	jq --argjson i "${index}" '."#select".tuples[$i][2] = 0' "${scratch}/positive.json" >"${scratch}/zero.json"
	reject zero
	jq --argjson i "${index}" 'del(."#select".tuples[$i])' "${scratch}/positive.json" >"${scratch}/missing.json"
	reject missing
done

jq '."#select".tuples[1] = ."#select".tuples[0]' "${scratch}/positive.json" >"${scratch}/duplicate.json"
reject duplicate
jq '."#select".tuples[3][0] = "desktop/env_darwin.go"' "${scratch}/positive.json" >"${scratch}/wrong-platform.json"
reject wrong-platform
jq '."#select".tuples[0][2] = "1"' "${scratch}/positive.json" >"${scratch}/string-count.json"
reject string-count
printf '{}\n' >"${scratch}/empty.json"
reject empty
printf '{invalid\n' >"${scratch}/malformed.json"
reject malformed

# Exercise the database path with valid body evidence even when a module failed.
# The CLI double supplies exports; the real guard decides whether CI may pass.
cat >"${scratch}/codeql" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${GUARD_MODE:-full}" == diagnostics && "$1" != database ]]; then exit 2; fi
case "$1 $2" in
"resolve qlpacks") printf '{"go":["/fixture/go"]}\n' ;;
"pack install" | "query run") ;;
"database export-diagnostics")
  [[ "$3" == --format=raw && "$4" == -- ]] || exit 2
  if [[ "${EXPORT_FAILURE:-false}" == true ]]; then exit 7; fi
  cat "${DIAGNOSTICS}"
  ;;
"bqrs decode")
  for argument in "$@"; do
    case "${argument}" in --output=*) cp "${BODY_RESULTS}" "${argument#--output=}" ;; esac
  done
  ;;
*) printf 'Unexpected CodeQL invocation\n' >&2; exit 2 ;;
esac
BASH
chmod +x "${scratch}/codeql"

# run_database keeps the guard's complete command path under test.
run_database() {
	local arguments=("${scratch}/database")
	if [[ "${2:-full}" == diagnostics ]]; then arguments=(--diagnostics-only "${scratch}/database"); fi
	CODEQL_CLI="${scratch}/codeql" BODY_RESULTS="${scratch}/positive.json" \
		DIAGNOSTICS="${scratch}/diagnostics.json" EXPORT_FAILURE="${1:-false}" \
		GUARD_MODE="${2:-full}" \
		bash "${validator}" "${arguments[@]}" >"${scratch}/output.log" 2>&1
}

# reject_database requires an extraction failure or unreadable export to stop CI.
reject_database() {
	local name="$1"
	local mode
	for mode in full diagnostics; do
		if run_database "${2:-false}" "${mode}"; then
			printf 'FAIL: accepted %s extraction diagnostics (%s)\n' "${name}" "${mode}" >&2
			exit 1
		fi
	done
}

printf '[]\n' >"${scratch}/diagnostics.json"
run_database
run_database false diagnostics
printf '[{"source":{"id":"go/extractor/warning","name":"Recoverable warning"},"severity":"warning"}]\n' \
	>"${scratch}/diagnostics.json"
run_database
run_database false diagnostics

# A killed module must fail despite all sampled CLI/desktop bodies surviving.
for project in . third_party/go-archive; do
	for severity in warning error note; do
		jq -n --arg project "${project}" --arg severity "${severity}" '[{
    source: {id: "go/autobuilder/extraction-failed-for-project", name: "Extraction failed"},
    severity: $severity, plaintextMessage: ("Extraction failed for " + $project + ": signal: killed")
  }]' >"${scratch}/diagnostics.json"
		reject_database "failed extraction for ${project} with valid body evidence"
		jq -e --arg project "${project}" '.message == ("Extraction failed for " + $project + ": signal: killed")' \
			<(head -n 1 "${scratch}/output.log") >/dev/null
	done
done

for invalid in '{}' 'null' '[{}]' '[{"source":{"id":null}}]' '[{"source":{"id":""}}]' '{invalid'; do
	printf '%s\n' "${invalid}" >"${scratch}/diagnostics.json"
	reject_database malformed
done
printf '{}\n[]\n' >"${scratch}/diagnostics.json"
reject_database multiple-documents
printf '[]\n' >"${scratch}/diagnostics.json"
reject_database export-command-failed true

printf 'All desktop CodeQL coverage cases passed.\n'
