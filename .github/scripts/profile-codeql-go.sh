#!/usr/bin/env bash
# Measure each standalone Go extraction, then independently verify database
# coverage. Only normalized measurements are exported; the database stays local.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
source "$script_dir/codeql-source.sh"

publish_report() {
  local inventory="$1" metrics="$2" status=0
  "$KSAIL_CODEQL_REPORTER" "$metrics" <"$inventory" >"$metrics/current.json" \
    2>"$metrics/report.err" || status=$?
  [[ "$status" == 0 || "$status" == 1 ]] || return 2
  jq -e '(.complete | type == "boolean") and (.records | type == "array")' "$metrics/current.json" >/dev/null
  jq -s '.[0] + .[1]' "$KSAIL_CODEQL_CONTEXT" "$metrics/current.json" >"$KSAIL_CODEQL_REPORT_PATH.tmp"
  mv "$KSAIL_CODEQL_REPORT_PATH.tmp" "$KSAIL_CODEQL_REPORT_PATH"
}

extract_projects() {
  local inventory="$1" metrics="$2" extractor="$3"
  local source_root="$PWD" project directory flags index=0 result=0 status base
  local time_flag project_list
  project_list="$(cat "$inventory")"
  case "$(uname -s)" in Linux) time_flag=-v ;; Darwin) time_flag=-l ;; *) return 2 ;; esac
  while IFS= read -r project; do
    flags=""
    case "$project" in
    root) directory="$source_root" ;;
    root-desktop)
      directory="$source_root"
      flags=-tags=desktop
      ;;
    *)
      [[ "$project" =~ ^[a-z0-9][a-z0-9_/-]*$ && "$project" != *..* && "$project" != *//* ]] || return 2
      directory="$source_root/$project"
      ;;
    esac
    [[ -f "$directory/go.mod" && ! -L "$directory" ]] || return 2
    printf -v base '%s/%05d' "$metrics" "$index"
    status=0
    (cd "$directory" && GOWORK=off GOFLAGS="$flags" /usr/bin/time "$time_flag" -o "$base.time" \
      "$extractor" -mod=mod ./...) || status=$?
    printf '%s\n' "$status" >"$base.exit"
    if [[ -n "${KSAIL_CODEQL_REPORTER:-}" ]]; then publish_report "$inventory" "$metrics"; fi
    if [[ "$status" != 0 ]]; then result="$status"; fi
    index=$((index + 1))
  done <<<"$project_list"
  return "$result"
}

if [[ $# == 4 && "$1" == --extract ]]; then
  extract_projects "$2" "$3" "$4"
  exit
fi
if [[ $# != 1 ]]; then
  printf 'Usage: %s OUTPUT_DIRECTORY\n' "$0" >&2
  exit 2
fi
: "${CODEQL_CLI:?An initialized CodeQL CLI is required}"
: "${GOMEMLIMIT:?The extraction heap limit must be explicit}"
[[ -x "$CODEQL_CLI" ]]
export GOWORK=off
source_root="$(git rev-parse --show-toplevel)"
cd "$source_root"
# A resource observation must name the bytes actually measured.
source_sha="$(git rev-parse HEAD)"
source_matches_head "$source_sha"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$1" "$scratch/metrics"
output="$(cd "$1" && pwd)"
inventory="$scratch/inventory"
# Exact basenames exclude authenticated upstream-go.mod archive copies.
while IFS= read -r module; do
  if [[ "$module" == go.mod ]]; then
    printf 'root\n'
  elif [[ "${module##*/}" == go.mod ]]; then
    printf '%s\n' "${module%/go.mod}"
  fi
done < <(git ls-files -- go.mod '*/go.mod') >"$inventory"
printf 'root-desktop\n' >>"$inventory"
go build -o "$scratch/report" ./internal/codeqlprofile/cmd

pack="$("$CODEQL_CLI" resolve extractor --language=go --format=json | jq -er 'select(type == "string")')"
cgroup_limit=null
case "$(uname -s)" in
Linux)
  platform=linux64
  memory="$(awk '/^MemTotal:/ {print $2 * 1024}' /proc/meminfo)"
  processors="$(getconf _NPROCESSORS_ONLN)"
  if [[ -r /sys/fs/cgroup/memory.max ]]; then
    value="$(cat /sys/fs/cgroup/memory.max)"
    if [[ "$value" =~ ^[0-9]+$ ]]; then cgroup_limit="$value"; fi
  fi
  ;;
Darwin)
  platform=osx64
  memory="$(sysctl -n hw.memsize)"
  processors="$(sysctl -n hw.ncpu)"
  ;;
*) exit 2 ;;
esac
extractor="$pack/tools/$platform/go-extractor"
[[ -x "$extractor" ]]
export CODEQL_EXTRACTOR_GO_ROOT="$pack" CODEQL_PLATFORM="$platform"
version="$("$CODEQL_CLI" version --format=json | jq -er '.version')"
export KSAIL_CODEQL_REPORTER="$scratch/report" KSAIL_CODEQL_CONTEXT="$scratch/context.json" \
  KSAIL_CODEQL_REPORT_PATH="$output/report.json"
jq -n --arg sha "$source_sha" --arg architecture "$(uname -m)" --arg platform "$platform" \
  --arg heapLimit "$GOMEMLIMIT" --arg version "$version" --arg goVersion "$(go env GOVERSION)" \
  --argjson physicalMemoryBytes "$memory" --argjson logicalProcessors "$processors" --argjson cgroupMemoryLimitBytes "$cgroup_limit" \
  '{sourceSha: $sha, architecture: $architecture, platform: $platform, codeqlVersion: $version,
	  goHeapLimit: $heapLimit, goVersion: $goVersion, physicalMemoryBytes: $physicalMemoryBytes,
	  logicalProcessors: $logicalProcessors, cgroupMemoryLimitBytes: $cgroupMemoryLimitBytes,
	  traceExitCode: -1, finalizeExitCode: -1, sourceVerified: false, coverageVerified: false}' >"$KSAIL_CODEQL_CONTEXT"
publish_report "$inventory" "$scratch/metrics"
# Earlier observations are already durable. A normal signal additionally records
# the latest process files, without converting interruption into completion.
trap 'publish_report "$inventory" "$scratch/metrics"; rm -rf "$scratch"' EXIT
trap 'exit 124' TERM INT
trace_status=0
finalize_status=-1
coverage=false
if "$CODEQL_CLI" database init --language=go --source-root="$source_root" "$scratch/database" &&
  "$CODEQL_CLI" database trace-command --no-tracing --working-dir="$source_root" "$scratch/database" -- \
    bash "$script_dir/profile-codeql-go.sh" --extract "$inventory" "$scratch/metrics" "$extractor"; then
  finalize_status=0
  "$CODEQL_CLI" database finalize --threads=2 --ram=4096 "$scratch/database" || finalize_status=$?
  if [[ "$finalize_status" == 0 ]] &&
    bash "$script_dir/verify-desktop-codeql.sh" "$scratch/database"; then coverage=true; fi
else
  trace_status=$?
fi
source_verified=false
if source_matches_head "$source_sha"; then source_verified=true; fi
jq --argjson traceExitCode "$trace_status" --argjson finalizeExitCode "$finalize_status" \
  --argjson coverage "$coverage" --argjson source "$source_verified" \
  '. + {traceExitCode: $traceExitCode, finalizeExitCode: $finalizeExitCode,
	  coverageVerified: $coverage, sourceVerified: $source}' "$KSAIL_CODEQL_CONTEXT" >"$scratch/context-final.json"
mv "$scratch/context-final.json" "$KSAIL_CODEQL_CONTEXT"
publish_report "$inventory" "$scratch/metrics"
jq -e '.complete and .sourceVerified and .coverageVerified and
	.traceExitCode == 0 and .finalizeExitCode == 0' "$output/report.json" >/dev/null
