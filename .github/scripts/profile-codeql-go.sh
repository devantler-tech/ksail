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
	if [[ -f "$metrics/memory.json" ]]; then
		jq -s '.[0] + .[1] + {memory:.[2]}' "$KSAIL_CODEQL_CONTEXT" "$metrics/current.json" "$metrics/memory.json" >"$KSAIL_CODEQL_REPORT_PATH.tmp"
	else
		jq -s '.[0] + .[1] + {memory:null}' "$KSAIL_CODEQL_CONTEXT" "$metrics/current.json" >"$KSAIL_CODEQL_REPORT_PATH.tmp"
	fi
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
			"$extractor" -mod=mod ./...) >"$base.log" 2>&1 || status=$?
		printf '%s\n' "$status" >"$base.exit"
		"${KSAIL_CODEQL_REPORTER:?}" --count-extracted-files "$source_root" <"$base.log" >"$base.files"
		publish_report "$inventory" "$metrics"
		printf 'Measured %s: exit=%s observed_file_completions=%s\n' "$project" "$status" "$(cat "$base.files")"
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
modules="$(git ls-files -- go.mod '*/go.mod')"
while IFS= read -r module; do
	if [[ "$module" == go.mod ]]; then
		printf 'root\n'
	elif [[ "${module##*/}" == go.mod ]]; then
		printf '%s\n' "${module%/go.mod}"
	fi
done <<<"$modules" >"$inventory"
printf 'root-desktop\n' >>"$inventory"
go build -o "$scratch/report" ./internal/codeqlprofile/cmd
go build -o "$scratch/memory" ./internal/codeqlprofile/cmd/memory

pack="$("$CODEQL_CLI" resolve extractor --language=go --format=json | jq -er 'select(type == "string")')"
cgroup_limit=null
cgroup_limits='{"memoryBytes":null,"swapBytes":null,"verified":false}'
case "$(uname -s)" in
Linux)
	platform=linux64
	memory="$(awk '/^MemTotal:/ {printf "%.0f", $2 * 1024}' "${KSAIL_CODEQL_PROC_ROOT:-/proc}/meminfo")"
	processors="$(getconf _NPROCESSORS_ONLN)"
	if observed_limits="$("$scratch/report" --cgroup-limits "${KSAIL_CODEQL_PROC_ROOT:-/proc}" /sys/fs/cgroup 2>"$scratch/cgroup.err")"; then
		cgroup_limits="$observed_limits"
		cgroup_limit="$(jq -c '.memoryBytes' <<<"$cgroup_limits")"
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
go_version="$(go env GOVERSION)"
architecture="$(uname -m)"
kernel_release="$(uname -r)"
extractor_hash="$(shasum -a 256 "$extractor" | cut -d ' ' -f 1)"
[[ "$extractor_hash" =~ ^[a-f0-9]{64}$ ]]
swap_metadata='null'
replica="${KSAIL_CODEQL_PROFILE_REPLICA:-0}"
[[ "$replica" =~ ^[0-2]$ ]]
if [[ -n "${KSAIL_CODEQL_SWAP_REPORT:-}" ]]; then
	swap_metadata="$(jq -ce 'select(type == "object")' "$KSAIL_CODEQL_SWAP_REPORT")"
fi
export KSAIL_CODEQL_REPORTER="$scratch/report" KSAIL_CODEQL_CONTEXT="$scratch/context.json" \
	KSAIL_CODEQL_REPORT_PATH="$output/report.json"
jq -n --arg sha "$source_sha" --arg architecture "$architecture" --arg platform "$platform" \
	--arg heapLimit "$GOMEMLIMIT" --arg version "$version" --arg goVersion "$go_version" \
	--arg imageOS "${ImageOS:-}" --arg imageVersion "${ImageVersion:-}" --arg kernel "$kernel_release" \
	--arg extractorHash "$extractor_hash" --arg runId "${GITHUB_RUN_ID:-}" --arg runAttempt "${GITHUB_RUN_ATTEMPT:-}" \
	--arg packagesHash "${KSAIL_CODEQL_PACKAGES_SHA256:-}" --arg processorHash "${KSAIL_CODEQL_PROCESSOR_SHA256:-}" \
	--argjson swap "$swap_metadata" \
	--argjson replica "$replica" \
	--argjson cgroups "$cgroup_limits" \
	--argjson physicalMemoryBytes "$memory" --argjson logicalProcessors "$processors" --argjson cgroupMemoryLimitBytes "$cgroup_limit" \
	'{sourceSha: $sha, architecture: $architecture, platform: $platform, codeqlVersion: $version,
	  goHeapLimit: $heapLimit, goVersion: $goVersion, physicalMemoryBytes: $physicalMemoryBytes,
	  logicalProcessors: $logicalProcessors, cgroupMemoryLimitBytes: $cgroupMemoryLimitBytes,
	  cgroupLimits:$cgroups, resourceEnvelopeVerified:false,
	  runnerImageOS:$imageOS, runnerImageVersion:$imageVersion, kernelRelease:$kernel,
	  extractorSha256:$extractorHash, workflowRunId:$runId, workflowRunAttempt:$runAttempt, swapPreparation:$swap,
	  runnerPackagesSha256:$packagesHash, processorModelSha256:$processorHash, replica:$replica,
	  profileExitCode:-1, memorySamplerExitCode:-1, nativeFiles:null, nativeFilesExitCode:-1,
	  traceExitCode: -1, finalizeExitCode: -1, sourceVerified: false, coverageVerified: false}' >"$KSAIL_CODEQL_CONTEXT"

sampler_pid=''
sampler_status=0
profile_started=$SECONDS
stop_sampler() {
	if [[ -n "$sampler_pid" ]]; then
		kill -TERM "$sampler_pid" 2>/dev/null || sampler_status=2
		wait "$sampler_pid" || sampler_status=$?
		sampler_pid=''
	fi
	jq --argjson status "$sampler_status" '.memorySamplerExitCode=$status' "$KSAIL_CODEQL_CONTEXT" >"$scratch/context-memory.json"
	mv "$scratch/context-memory.json" "$KSAIL_CODEQL_CONTEXT"
}

finish_profile() {
	local status=$?
	trap - EXIT
	stop_sampler || status=2
	jq --argjson status "$status" --argjson elapsed "$((SECONDS - profile_started))" \
		'.profileExitCode=$status | .profileElapsedSeconds=$elapsed' "$KSAIL_CODEQL_CONTEXT" >"$scratch/context-exit.json" || status=2
	mv "$scratch/context-exit.json" "$KSAIL_CODEQL_CONTEXT" || status=2
	publish_report "$inventory" "$scratch/metrics" || status=2
	rm -rf "$scratch"
	exit "$status"
}
# Earlier observations are already durable. A normal signal additionally records
# the latest process files, without converting interruption into completion.
trap finish_profile EXIT
trap 'exit 124' TERM INT
if [[ "$platform" == linux64 ]]; then
	"$scratch/memory" "$scratch/metrics/memory.json" &
	sampler_pid=$!
fi
publish_report "$inventory" "$scratch/metrics"
trace_status=0
finalize_status=-1
coverage=false
native_files=null
native_status=-1
if "$CODEQL_CLI" database init --language=go --source-root="$source_root" "$scratch/database" &&
	"$CODEQL_CLI" database trace-command --no-tracing --working-dir="$source_root" "$scratch/database" -- \
		bash "$script_dir/profile-codeql-go.sh" --extract "$inventory" "$scratch/metrics" "$extractor"; then
	finalize_status=0
	"$CODEQL_CLI" database finalize --threads=2 --ram=4096 "$scratch/database" || finalize_status=$?
	if [[ "$finalize_status" == 0 ]]; then
		if bash "$script_dir/verify-desktop-codeql.sh" "$scratch/database"; then coverage=true; fi
		native_status=0
		native_files="$(bash "$script_dir/profile-codeql-files.sh" "$scratch/database")" || native_status=$?
		if [[ "$native_status" != 0 ]]; then native_files=null; fi
	fi
else
	trace_status=$?
fi
source_verified=false
if source_matches_head "$source_sha"; then source_verified=true; fi
envelope_verified=false
if [[ "$platform" == linux64 ]] && after_limits="$("$scratch/report" --cgroup-limits "${KSAIL_CODEQL_PROC_ROOT:-/proc}" /sys/fs/cgroup 2>"$scratch/cgroup-after.err")" &&
	jq -e --argjson after "$after_limits" '.cgroupLimits.verified and $after.verified and .cgroupLimits == $after' "$KSAIL_CODEQL_CONTEXT" >/dev/null; then
	envelope_verified=true
fi
stop_sampler
jq --argjson traceExitCode "$trace_status" --argjson finalizeExitCode "$finalize_status" \
	--argjson nativeFiles "$native_files" --argjson nativeFilesExitCode "$native_status" \
	--argjson envelope "$envelope_verified" \
	--argjson coverage "$coverage" --argjson source "$source_verified" \
	'. + {traceExitCode: $traceExitCode, finalizeExitCode: $finalizeExitCode,
	  nativeFiles:$nativeFiles,nativeFilesExitCode:$nativeFilesExitCode,
	  resourceEnvelopeVerified:$envelope,
	  coverageVerified: $coverage, sourceVerified: $source}' "$KSAIL_CODEQL_CONTEXT" >"$scratch/context-final.json"
mv "$scratch/context-final.json" "$KSAIL_CODEQL_CONTEXT"
publish_report "$inventory" "$scratch/metrics"
jq -e '.complete and .measurementsValid and .sourceVerified and .coverageVerified and
	.traceExitCode == 0 and .finalizeExitCode == 0 and .nativeFilesExitCode == 0 and
	(.nativeFiles.sourceGoFiles > 0) and .memorySamplerExitCode == 0 and
	(.platform != "linux64" or (.memory.sampleCount > 0))' "$output/report.json" >/dev/null
