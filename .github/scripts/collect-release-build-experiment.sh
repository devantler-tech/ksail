#!/usr/bin/env bash
# One protected-main, cold build. This script never publishes its binaries.
set -euo pipefail
export LC_ALL=C TZ=UTC NO_COLOR=1 TERM=dumb
helper=$(realpath "$1")
source_dir=$(realpath "$2")
report_dir=$(realpath "$3")
pair=$4 arm=$5
printf '{"complete":false}\n' > "$report_dir/report.json"
[[ ${GITHUB_REPOSITORY:-} == devantler-tech/ksail && ${GITHUB_REF:-} == refs/heads/main ]]
[[ ${GITHUB_EVENT_NAME:-} == workflow_dispatch && ${GITHUB_RUN_ATTEMPT:-} == 1 ]]
[[ $pair =~ ^[123]$ && ( $arm == baseline || $arm == candidate ) ]]
[[ ${RUNNER_OS:-} == Linux && ${GOMEMLIMIT:-} == 6GiB && ! -v GOMAXPROCS ]]
"$helper" runtime > "$report_dir/runtime.json" # also refuses publication/cloud credentials
"$helper" limits > "$report_dir/limits.json"
cd "$source_dir"
expected=a3a3957d45f8071c3be2fd2fc96e699459d01d73
[[ $(git rev-parse HEAD) == "$expected" && $(git rev-parse 'v7.206.65^{commit}') == "$expected" ]]
[[ -z $(git status --porcelain --untracked-files=all) ]]
[[ $(git hash-object --no-filters .goreleaser.yaml) == 91dd87214face749e2f061e59bd9ead039720c56 ]]
[[ $(go env GOVERSION) == go1.27.3 ]]
goreleaser --version | grep -Eq '^GitVersion:[[:space:]]+2\.19\.0$'
"$helper" prepare-config "$arm" .goreleaser.yaml "$report_dir/config.yaml"

# The harness/tool installation uses separate caches. Both measured arms begin
# with empty build AND module caches, with no shared cache save or restoration.
[[ -n ${GOCACHE:-} && -n ${GOMODCACHE:-} && -n ${GOTMPDIR:-} ]]
for directory in "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR"; do
  [[ -d $directory && -z $(find "$directory" -mindepth 1 -print -quit) ]]
done

group=$(awk -F: '$1=="0" && $2=="" {print $3}' /proc/self/cgroup)
[[ $group == /* && $group != *..* ]]
cgroup=$(realpath "/sys/fs/cgroup$group")
[[ $cgroup == /sys/fs/cgroup || $cgroup == /sys/fs/cgroup/* ]]
# A missing aggregate peak/event/CPU read is unknown, never zero or success.
number() { [[ $1 =~ ^[0-9]+$ ]]; }
metric() { awk -v key="$2" '$1==key {print $2}' "$1"; }
memory_peak=$(cat "$cgroup/memory.peak"); number "$memory_peak"
oom_before=$(metric "$cgroup/memory.events" oom_kill); number "$oom_before"
cpu_before=$(metric "$cgroup/cpu.stat" usage_usec); number "$cpu_before"
swap_before=$(metric /proc/vmstat pswpout); number "$swap_before"

dpkg-query -W -f='${Package}=${Version}\n' | sort > "$report_dir/packages.txt"
awk -F: '/^(vendor_id|model name|flags)[[:space:]]*:/ {print}' /proc/cpuinfo | sort -u > "$report_dir/processor.txt"
test -s "$report_dir/processor.txt"
constraints() {
  grep -E '^(Cpus_allowed_list|Mems_allowed_list):' /proc/self/status
  directory=$cgroup
  while :; do
    for name in cpu.max cpuset.cpus.effective cpuset.mems.effective; do
      if [[ -f $directory/$name ]]; then printf '%s=' "$name"; cat "$directory/$name";
      elif [[ $directory != /sys/fs/cgroup ]]; then exit 1; fi
    done
    [[ $directory != /sys/fs/cgroup ]] || break
    directory=$(dirname "$directory")
  done
}
constraints > "$report_dir/constraints.txt"
package_hash=$(sha256sum "$report_dir/packages.txt"); package_hash=${package_hash%% *}
processor_hash=$(sha256sum "$report_dir/processor.txt"); processor_hash=${processor_hash%% *}
constraints_hash=$(sha256sum "$report_dir/constraints.txt"); constraints_hash=${constraints_hash%% *}
physical_memory=$(awk '$1=="MemTotal:" {printf "%.0f",$2*1024}' /proc/meminfo); number "$physical_memory"
host_swap=$(awk '$1=="SwapTotal:" {printf "%.0f",$2*1024}' /proc/meminfo); number "$host_swap"
node_version=$(node --version); npm_version=$(npm --version)
jq -n --slurpfile runtime "$report_dir/runtime.json" --slurpfile limits "$report_dir/limits.json" \
  --arg image "${ImageOS:?}" --arg image_version "${ImageVersion:?}" --arg processor "$processor_hash" \
  --arg packages "$package_hash" --arg constraints "$constraints_hash" --arg kernel "$(uname -r)" \
  --argjson physical "$physical_memory" --argjson host_swap "$host_swap" --arg node "$node_version" --arg npm "$npm_version" \
  '{runnerImage:$image,runnerVersion:$image_version,processorHash:$processor,packagesHash:$packages,
    constraintsHash:$constraints,physicalMemoryBytes:$physical,hostSwapBytes:$host_swap,kernel:$kernel,goVersion:$runtime[0].goVersion,
    goreleaserVersion:"2.19.0",outerParallelism:4,defaultChildProcs:$runtime[0].defaultChildProcs,
    goMemLimit:"6GiB",nodeVersion:$node,npmVersion:$npm,limits:$limits[0]}' > "$report_dir/environment.json"

# shellcheck source=.github/scripts/release-build-samples.lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/release-build-samples.lib.sh"
sample_disk() {
  while :; do
    # Sample every filesystem carrying source, modules, cache or scratch. This
    # records sampled headroom, not an assertion about between-sample minima.
    sample_once "$source_dir" "$report_dir"
    sleep 5
  done
}
sample_disk & sampler=$!
stop_sampler() { kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true; }
trap stop_sampler EXIT
started=$(date +%s.%N)
status=0
# timeout owns its process group, including hooks and compilers; the second
# deadline kills that same group if a process ignores TERM. No snapshot/skip.
timeout --signal=TERM --kill-after=30s 55m \
  goreleaser build --clean --config "$report_dir/config.yaml" --parallelism 4 --timeout 55m 2>&1 |
  while IFS= read -r line; do
    printf '%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)" "$line" >> "$report_dir/build.log"
    printf '%s\n' "$line"
  done || status=$?
finished=$(date +%s.%N)
kill -0 "$sampler"
kill "$sampler"
sampler_status=0
wait "$sampler" || sampler_status=$?
[[ $sampler_status == 143 ]]
sample_once "$source_dir" "$report_dir"
trap - EXIT
"$helper" limits > "$report_dir/limits-after.json"
cmp "$report_dir/limits.json" "$report_dir/limits-after.json"
constraints > "$report_dir/constraints-after.txt"
cmp "$report_dir/constraints.txt" "$report_dir/constraints-after.txt"
[[ $(awk '$1=="SwapTotal:" {printf "%.0f",$2*1024}' /proc/meminfo) == "$host_swap" ]]
cpu_after=$(metric "$cgroup/cpu.stat" usage_usec); number "$cpu_after"
oom_after=$(metric "$cgroup/memory.events" oom_kill); number "$oom_after"
swap_after=$(metric /proc/vmstat pswpout); number "$swap_after"
memory_peak=$(cat "$cgroup/memory.peak"); number "$memory_peak"
disk_minimum=$(awk '{print $2}' "$report_dir/disk-samples" | sort -n | head -n 1); number "$disk_minimum"
sample_count=$(wc -l < "$report_dir/disk-samples")
elapsed=$(awk -v start="$started" -v end="$finished" 'BEGIN {print end-start}')
validate_disk_samples "$report_dir/disk-samples" "$started" "$finished"
[[ $cpu_after -ge $cpu_before && $oom_after -ge $oom_before && $swap_after -ge $swap_before ]]
# Normalize actual failures before any semantic validation can fail. A failed
# build is preserved and cannot later be replaced inside this cohort.
jq -n --slurpfile environment "$report_dir/environment.json" --argjson pair "$pair" --arg arm "$arm" \
  --arg harness "${GITHUB_SHA:?}" --arg run "${GITHUB_RUN_ID:?}" --arg attempt "${GITHUB_RUN_ATTEMPT:?}" \
  --argjson elapsed "$elapsed" --argjson peak "$memory_peak" --argjson disk "$disk_minimum" \
  --argjson count "$sample_count" --argjson cpu "$((cpu_after-cpu_before))" \
  --argjson oom "$((oom_after-oom_before))" --argjson swap "$((swap_after-swap_before))" \
  '{schemaVersion:1,complete:false,pair:$pair,arm:$arm,sourceSha:"a3a3957d45f8071c3be2fd2fc96e699459d01d73",
    tag:"v7.206.65",configBlob:"91dd87214face749e2f061e59bd9ead039720c56",harnessSha:$harness,runId:$run,attempt:$attempt,
    environment:$environment[0],childProcs:(if $arm=="candidate" then 1 else $environment[0].defaultChildProcs end),
    elapsedSeconds:$elapsed,binaryBuildSeconds:0,
    resources:{complete:true,scope:"job-cgroup-peak;host-swap;sampled-disk",sampleIntervalSeconds:5,
      cpuUsageMicros:$cpu,memoryPeakBytes:$peak,minimumFreeDiskBytes:$disk,oomKills:$oom,swapOutPages:$swap,sampleCount:$count},
    inputs:{},nativeCli:{},outputs:[]}' > "$report_dir/report.json"
[[ $status == 0 ]]
"$helper" phase "$report_dir/build.log" > "$report_dir/phase.json"
commit_date=$(git show -s --format=%cI HEAD)
commit_date=$(date -u -d "$commit_date" +%Y-%m-%dT%H:%M:%SZ)
"$helper" inspect dist > "$report_dir/outputs.json"
[[ -z $(git status --porcelain --untracked-files=no) ]]

# Include the actual staged UI and all generated, platform-bound Copilot bundles.
tree_hash() {
  local directory=$1 pattern=$2
  (cd "$directory"; find . -type f -name "$pattern" -print0 | sort -z | xargs -0 -r sha256sum) | sha256sum | cut -d' ' -f1
}
[[ $(find pkg/webui/dist -type f ! -name .gitkeep | wc -l) -gt 0 ]]
[[ $(find . -maxdepth 1 -type f -name '*1.0.87*' | wc -l) -ge 5 ]]
ui_hash=$(tree_hash pkg/webui/dist '*')
bundler_hash=$(tree_hash . '*1.0.87*')
linux_binary=$(find dist -type f -path '*linux_amd64*' -name ksail)
[[ -n $linux_binary && $linux_binary != *$'\n'* ]]
timeout 30s "$linux_binary" --version > "$report_dir/version.txt"
grep -Fq 7.206.65 "$report_dir/version.txt"
grep -Fq "$expected" "$report_dir/version.txt"
grep -Fq "$commit_date" "$report_dir/version.txt"
timeout 30s "$linux_binary" --help > "$report_dir/help.txt"
grep -Fq Usage "$report_dir/help.txt"
timeout 30s "$linux_binary" workload apply --help | grep -Fq -- --schema-location-mode
jq --slurpfile phase "$report_dir/phase.json" --slurpfile outputs "$report_dir/outputs.json" \
  --arg ui "$ui_hash" --arg bundler "$bundler_hash" \
  '.binaryBuildSeconds=$phase[0]|.outputs=$outputs[0]|.inputs={uiHash:$ui,bundlerHash:$bundler}|
    .nativeCli={version:true,help:true,schemaFlag:true}|.complete=true' \
  "$report_dir/report.json" > "$report_dir/complete.json"
mv "$report_dir/complete.json" "$report_dir/report.json"
