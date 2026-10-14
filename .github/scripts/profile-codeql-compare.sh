#!/usr/bin/env bash
# A valid failed baseline is an observation, never a green extraction verdict.
set -euo pipefail
[[ $# == 4 && ("$1" == validate || "$1" == compare) ]]
mode="$1" sha="$2"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ "$sha" =~ ^[a-f0-9]{40}$ ]]
head="$(git rev-parse HEAD)"
[[ "$head" == "$sha" ]]
: "${GITHUB_RUN_ID:?The originating workflow run must be explicit}"
: "${GITHUB_RUN_ATTEMPT:?The originating attempt must be explicit}"
modules="$(git ls-files -- go.mod '*/go.mod')"
inventory="$(printf '%s\n' "$modules" | jq -Rsc '
  split("\n")|map(select(. == "go.mod" or endswith("/go.mod")))|
  map(if . == "go.mod" then "root" else rtrimstr("/go.mod") end) + ["root-desktop"]')"
jq -e 'length == 14 and (unique|length)==14 and .[0]=="root" and .[-1]=="root-desktop"' <<<"$inventory" >/dev/null

validate_observation() {
	local arm="$1" report="$2"
	[[ "$arm" == 0 || "$arm" == 8 ]]
	jq -se --arg sha "$sha" --argjson arm "$arm" --argjson inventory "$inventory" \
		--arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" '
    def uint: type == "number" and . >= 0 and floor == .;
    def text: type == "string" and length > 0 and (explode|all(. >= 32 and . != 127));
    def code: uint and . <= 255;
    def exitcode: . == -1 or code;
    def passed: .complete and .coverageVerified and .sourceVerified and .traceExitCode==0 and
      .finalizeExitCode==0 and .nativeFilesExitCode==0;
    length == 1 and (.[0] as $r | $r |
      .sourceSha==$sha and .workflowRunId==$run and .workflowRunAttempt==$attempt and
      .goHeapLimit=="off" and .platform=="linux64" and
      all([.architecture,.runnerImageOS,.runnerImageVersion,.kernelRelease,.goVersion,.codeqlVersion][]; text) and
      (.extractorSha256|type=="string" and test("^[0-9a-f]{64}$")) and
      all([.runnerPackagesSha256,.processorModelSha256][]; type=="string" and test("^[0-9a-f]{64}$")) and
      (.physicalMemoryBytes|uint and .>0) and (.logicalProcessors|uint and .>0) and
      .cgroupLimits.verified==true and .resourceEnvelopeVerified==true and
      (.cgroupLimits|has("memoryBytes") and has("swapBytes")) and has("cgroupMemoryLimitBytes") and
      (.cgroupLimits.memoryBytes|null == . or uint) and (.cgroupLimits.swapBytes|null == . or uint) and
      .cgroupMemoryLimitBytes==.cgroupLimits.memoryBytes and
      .sourceVerified==true and .measurementsValid==true and .memorySamplerExitCode==0 and
      .expectedProjects==$inventory and (.records|type=="array" and length==14) and
      ([.records[].project]|sort)==($inventory|sort) and
      all(.records[]; (.exitCode|code) and (.observedExtractedFileCount|uint) and
        (.stats.commandPeakRssBytes|uint and .>0) and (.stats.wallSeconds|type=="number" and .>=0 and .<=6000) and
        (.stats.timeExitCode|code)) and
      (.complete|type=="boolean") and .complete==all(.records[]; .exitCode==0 and .stats.timeExitCode==0) and
      (.profileExitCode|code) and (.traceExitCode|exitcode) and (.finalizeExitCode|exitcode) and
      (.coverageVerified|type=="boolean") and (.nativeFilesExitCode|exitcode) and
      .swapPreparation.allocationVerified==true and .swapPreparation.requestedAddedSwapGiB==$arm and
      (.swapPreparation.originalSwapBytes|uint) and (.swapPreparation.preparedSwapBytes|uint) and
      (.swapPreparation.activatedSwapBytes|uint) and (.swapPreparation.allocatedFileBytes|uint) and
      .swapPreparation.preparedSwapBytes==(.swapPreparation.originalSwapBytes+.swapPreparation.activatedSwapBytes) and
      (if $arm==0 then .swapPreparation.activatedSwapBytes==0 and .swapPreparation.allocatedFileBytes==0 else
        .swapPreparation.allocatedFileBytes==8589934592 and
        (.swapPreparation.activatedSwapBytes>=8588886016 and .swapPreparation.activatedSwapBytes<=8589934592) end) and
      (.profileElapsedSeconds|uint and .>0 and .<=6600) and
      (.memory.sampleCount|uint and .>=2) and (.memory.lastElapsedSeconds|type=="number" and .>0) and
      .memory.lastElapsedSeconds >= (.profileElapsedSeconds-10) and .memory.lastElapsedSeconds <= (.profileElapsedSeconds+10) and
      .memory.sampleCount >= (.profileElapsedSeconds/10|floor) and
      .memory.totalBytes==.physicalMemoryBytes and .memory.swapTotalBytes==.swapPreparation.preparedSwapBytes and
      (.memory.minimumAvailableBytes|uint and .<=$r.physicalMemoryBytes) and
      (.memory.maximumSwapUsedBytes|uint and .<=$r.swapPreparation.preparedSwapBytes) and
      (.memory.largestSampledRssBytes|uint and .>0) and
      (if .finalizeExitCode==0 then .nativeFilesExitCode==0 and
        (.nativeFiles.sourceGoFiles|uint and .>0) and (.nativeFiles.desktopGoFiles|uint and .>0) and
        .nativeFiles.desktopGoFiles<=.nativeFiles.sourceGoFiles and
        (.nativeFiles.sourceFilesSha256|type=="string" and test("^[0-9a-f]{64}$")) else
        .nativeFilesExitCode == -1 and .nativeFiles == null and .coverageVerified == false end) and
      ((.profileExitCode==0)==passed) and
      (if passed then ([.records[].observedExtractedFileCount]|add)>=.nativeFiles.sourceGoFiles else true end) and
      (if $arm==8 then passed else true end))
  ' "$report" >/dev/null || {
		printf '::error::Incomplete, invalid or misbound resource observation for swap arm %s.\n' "$arm" >&2
		return 1
	}
}

if [[ "$mode" == validate ]]; then
	validate_observation "$3" "$4"
	printf 'Validated collection for swap arm %s; extraction outcome remains in the report.\n' "$3"
	exit
fi
validate_observation 0 "$3"
validate_observation 8 "$4"
jq -L "$script_dir" -n --slurpfile baseline "$3" --slurpfile treatment "$4" '
  include "profile-codeql-environment";
  $baseline[0] as $b | $treatment[0] as $t |
  if ($b|profile_environment) != ($t|profile_environment) then error("runner, tools, source or original swap differs between arms") else
    ($b.profileExitCode==0) as $complete |
    if $complete and $b.nativeFiles != $t.nativeFiles then error("native database file sets differ") else
      {collectionValid:true,comparableCoverage:$complete,
        outcome:(if $complete then "both-complete" else "baseline-incomplete" end),
        baseline:$b,treatment:$t,
        projects:(if $complete then [$b.records[] as $before | $t.records[] | select(.project==$before.project) |
          {project,before:$before.stats,after:.stats}] else null end)}
    end
  end'
