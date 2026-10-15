#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/source/desktop"
(
	cd "$scratch/source"
	printf 'module example.invalid/compare\n' >go.mod
	printf 'package main\n' >main.go
	printf 'package main\n' >desktop/main.go
	for number in {1..12}; do
		printf -v module 'third_party/mod%02d' "$number"
		mkdir -p "$module"
		printf 'module example.invalid/mod%s\n' "$number" >"$module/go.mod"
	done
	git init -q
	git add go.mod main.go desktop/main.go third_party
	git -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm fixture
)
cd "$scratch/source"
sha="$(git rev-parse HEAD)"
export GITHUB_RUN_ID=12345 GITHUB_RUN_ATTEMPT=1
inventory='["root","third_party/mod01","third_party/mod02","third_party/mod03","third_party/mod04","third_party/mod05","third_party/mod06","third_party/mod07","third_party/mod08","third_party/mod09","third_party/mod10","third_party/mod11","third_party/mod12","root-desktop"]'
jq -n --arg sha "$sha" --argjson inventory "$inventory" '{
 sourceSha:$sha,workflowRunId:"12345",workflowRunAttempt:"1",architecture:"x86_64",platform:"linux64",
 codeqlVersion:"2.27.1",goVersion:"go1.26.8",goHeapLimit:"off",extractorSha256:("a"*64),
 runnerPackagesSha256:("c"*64),processorModelSha256:("d"*64),
 physicalMemoryBytes:16777216000,logicalProcessors:4,cgroupMemoryLimitBytes:null,
 cgroupLimits:{memoryBytes:null,swapBytes:null,verified:true},resourceEnvelopeVerified:true,
 runnerImageOS:"ubuntu24",runnerImageVersion:"fixture-image",kernelRelease:"fixture-kernel",
 swapPreparation:{requestedAddedSwapGiB:0,originalSwapBytes:3221225472,preparedSwapBytes:3221225472,
   activatedSwapBytes:0,allocatedFileBytes:0,allocationVerified:true},
 memory:{sampleCount:10,lastElapsedSeconds:45,totalBytes:16777216000,swapTotalBytes:3221225472,
   minimumAvailableBytes:1024,maximumSwapUsedBytes:1024,largestSampledRssBytes:2048},memorySamplerExitCode:0,
 complete:true,measurementsValid:true,sourceVerified:true,coverageVerified:true,
 traceExitCode:0,finalizeExitCode:0,nativeFilesExitCode:0,profileExitCode:0,profileElapsedSeconds:50,
 nativeFiles:{sourceGoFiles:2,desktopGoFiles:1,sourceFilesSha256:("b"*64)},
 expectedProjects:$inventory,records:[$inventory[]|{project:.,exitCode:0,observedExtractedFileCount:1,
   stats:{commandPeakRssBytes:1024,wallSeconds:1,timeExitCode:0}}]}' >"$scratch/before.json"
jq '.swapPreparation += {requestedAddedSwapGiB:8,preparedSwapBytes:11811155968,
  activatedSwapBytes:8589930496,allocatedFileBytes:8589934592} | .memory.swapTotalBytes=11811155968' \
	"$scratch/before.json" >"$scratch/after.json"
bash "$script_dir/profile-codeql-compare.sh" validate "$sha" 0 "$scratch/before.json"
bash "$script_dir/profile-codeql-compare.sh" validate "$sha" 8 "$scratch/after.json"
bash "$script_dir/profile-codeql-compare.sh" compare "$sha" "$scratch/before.json" "$scratch/after.json" >"$scratch/pair.json"
jq -e '.collectionValid and .comparableCoverage and .outcome == "both-complete" and (.projects|length)==14' "$scratch/pair.json" >/dev/null

jq '.complete=false | .profileExitCode=1 | .traceExitCode=137 | .finalizeExitCode=-1 |
  .coverageVerified=false | .nativeFilesExitCode=-1 | .nativeFiles=null | .records[0].exitCode=137' \
	"$scratch/before.json" >"$scratch/failed.json"
bash "$script_dir/profile-codeql-compare.sh" compare "$sha" "$scratch/failed.json" "$scratch/after.json" >"$scratch/failed-pair.json"
jq -e '.collectionValid and (.comparableCoverage|not) and .outcome == "baseline-incomplete" and
  .baseline.records[0].exitCode == 137 and .baseline.nativeFiles == null and (.projects == null)' "$scratch/failed-pair.json" >/dev/null

for mutation in '.sourceVerified=false' '.memorySamplerExitCode=23' '.memory.sampleCount=0' '.memory.lastElapsedSeconds=1' \
	'.records=.records[0:13]' '.records[0].observedExtractedFileCount=null' \
	'.records[0].stats.commandPeakRssBytes=0' '.swapPreparation.allocationVerified=false' \
	'.cgroupLimits.verified=false' '.resourceEnvelopeVerified=false' '.workflowRunId="other"' \
	'.nativeFilesExitCode=23' '.nativeFiles.sourceFilesSha256="invalid"' \
	'del(.cgroupLimits.memoryBytes)' 'del(.cgroupMemoryLimitBytes)' \
	'del(.runnerPackagesSha256)' '.processorModelSha256="unknown"'; do
	jq "$mutation" "$scratch/before.json" >"$scratch/invalid.json"
	if bash "$script_dir/profile-codeql-compare.sh" validate "$sha" 0 "$scratch/invalid.json" >"$scratch/output" 2>&1; then
		printf 'FAIL: malformed/incomplete observation accepted: %s\n' "$mutation" >&2
		exit 1
	fi
done
for mutation in '.runnerImageVersion="other"' '.goVersion="go1.26.9"' \
	'.runnerPackagesSha256=("e"*64)' '.processorModelSha256=("f"*64)' \
	'.physicalMemoryBytes+=1024 | .memory.totalBytes+=1024' \
	'.nativeFiles.sourceFilesSha256=("c"*64)' '.swapPreparation.originalSwapBytes+=1024 | .swapPreparation.preparedSwapBytes+=1024 | .memory.swapTotalBytes+=1024'; do
	jq "$mutation" "$scratch/after.json" >"$scratch/unequal.json"
	if bash "$script_dir/profile-codeql-compare.sh" compare "$sha" "$scratch/before.json" "$scratch/unequal.json" >"$scratch/output" 2>&1; then
		printf 'FAIL: unequal pair accepted: %s\n' "$mutation" >&2
		exit 1
	fi
done
printf 'PASS: comparable complete pair, retained failed baseline and rejected invalid/unequal observations\n'

# A fixed cohort selects by environment before examining extraction outcomes.
cohort="$scratch/cohort"
mkdir -p "$cohort"
reports=()
for replica in 0 1 2; do
	for arm in 0 8; do
		directory="$cohort/codeql-go-profile-$sha-attempt1-swap$arm-replica$replica"
		mkdir -p "$directory"
		fixture="$scratch/before.json"
		if [[ "$arm" == 8 ]]; then fixture="$scratch/after.json"; fi
		jq --argjson replica "$replica" '.replica=$replica' "$fixture" >"$directory/report.json"
		reports+=("$directory/report.json")
	done
done
bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/cohort.json"
jq -e '.collectionValid and .comparableCoverage and (.observations|length)==6 and
  .selection.baseline.replica==0 and .selection.treatment.replica==0' "$scratch/cohort.json" >/dev/null

jq -L "$script_dir" -s -f "$script_dir/profile-codeql-cohort-select.jq" "${reports[@]}" >"$scratch/selected.json"
jq -s '.[] | .profileExitCode=137 | .complete=false | .coverageVerified=false | .nativeFiles=null |
  .records |= map(.exitCode=137 | .stats.commandPeakRssBytes=999999 | .stats.wallSeconds=99)' \
	"${reports[@]}" >"$scratch/changed-outcomes.json"
jq -L "$script_dir" -s -f "$script_dir/profile-codeql-cohort-select.jq" "$scratch/changed-outcomes.json" >"$scratch/selected-after.json"
cmp "$scratch/selected.json" "$scratch/selected-after.json"
jq -s '.[] | .processorModelSha256=(if .replica==1 then "a"*64 elif .replica==2 then "e"*64 else "f"*64 end)' \
	"${reports[@]}" >"$scratch/ordered-environments.json"
jq -L "$script_dir" -s -f "$script_dir/profile-codeql-cohort-select.jq" "$scratch/ordered-environments.json" >"$scratch/ordered-selection.json"
jq -e '(.cohorts|length)==3 and .baseline.replica==1 and .treatment.replica==1' "$scratch/ordered-selection.json" >/dev/null

baseline="${reports[0]}"
cp "$baseline" "$scratch/saved-baseline.json"
jq '.replica=0' "$scratch/failed.json" >"$baseline"
bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/cohort-failed-baseline.json"
jq -e '.outcome=="baseline-incomplete" and .selection.baseline.replica==0 and
  .baseline.profileExitCode==1 and (.observations|length)==6 and .projects==null' "$scratch/cohort-failed-baseline.json" >/dev/null
cp "$scratch/saved-baseline.json" "$baseline"

unselected="${reports[5]}"
cp "$unselected" "$scratch/saved-unselected.json"
for mutation in '.replica=0' '.workflowRunId="other"' '.workflowRunAttempt="2"' '.sourceSha=("b"*40)' \
	'.swapPreparation.requestedAddedSwapGiB=0' '.nativeFiles.sourceFilesSha256=("f"*64)' \
	'.complete=false | .profileExitCode=1 | .traceExitCode=137 | .finalizeExitCode=-1 | .coverageVerified=false | .nativeFilesExitCode=-1 | .nativeFiles=null | .records[0].exitCode=137'; do
	jq "$mutation" "$scratch/saved-unselected.json" >"$unselected"
	if bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/output" 2>&1; then
		printf 'FAIL: invalid unselected cohort member accepted: %s\n' "$mutation" >&2
		exit 1
	fi
done
cp "$scratch/saved-unselected.json" "$unselected"
mv "$unselected" "$scratch/missing-report.json"
if bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/output" 2>&1; then
	printf 'FAIL: missing cohort member accepted\n' >&2
	exit 1
fi
mv "$scratch/missing-report.json" "$unselected"
mkdir "$cohort/unexpected"
cp "$unselected" "$cohort/unexpected/report.json"
if bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/output" 2>&1; then
	printf 'FAIL: extra cohort report accepted\n' >&2
	exit 1
fi
rm "$cohort/unexpected/report.json"
rmdir "$cohort/unexpected"

for report in "${reports[@]}"; do
	jq '.processorModelSha256=((if .swapPreparation.requestedAddedSwapGiB==0 then .replica|tostring else ["a","b","c"][.replica] end)*64)' \
		"$report" >"$scratch/changed.json"
	mv "$scratch/changed.json" "$report"
done
if bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/output" 2>"$scratch/error"; then
	printf 'FAIL: cohort without a matching environment accepted\n' >&2
	exit 1
fi
jq -e '.collectionValid and (.comparableCoverage|not) and .outcome=="inconclusive-no-matching-environment" and
  (.observations|length)==6 and (.selection.cohorts|length)==6 and .selection.baseline==null and .projects==null' "$scratch/output" >/dev/null
jq '.memorySamplerExitCode=23' "$unselected" >"$scratch/changed.json"
mv "$scratch/changed.json" "$unselected"
if bash "$script_dir/profile-codeql-cohort.sh" "$sha" "$cohort" >"$scratch/output" 2>"$scratch/error"; then
	printf 'FAIL: invalid unmatched cohort member accepted\n' >&2
	exit 1
fi
if jq -e '.collectionValid==true' "$scratch/output" >/dev/null 2>&1; then
	printf 'FAIL: unmatched invalid observations certified as valid\n' >&2
	exit 1
fi
printf 'PASS: fixed cohort, outcome-independent selection, retained failed baseline and all-member refusal controls\n'
