#!/usr/bin/env bash
# Exercise the frozen release configuration and adversarial cohort admission.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture="$here/../tests/fixtures/release-build-experiment/goreleaser.yaml"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
cd "$here/../.."
helper="$scratch/release-build-experiment"
go build -o "$helper" ./internal/releasebuildprofile/cmd

# Positive controls do not inherit a caller's publication credentials.
call() {
	env -u GH_TOKEN -u GITHUB_TOKEN -u HOMEBREW_GITHUB_API_TOKEN -u GPG_PRIVATE_KEY \
		-u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY -u AWS_SESSION_TOKEN -u DOCKER_PASSWORD -u GHCR_TOKEN \
		"$helper" "$@"
}

# Invalid input must fail rather than silently produce an affirmative result.
reject() {
	if "$@" >"$scratch/rejected.log" 2>&1; then
		printf 'FAIL: invalid experiment accepted\n' >&2
		exit 1
	fi
}

call prepare-config baseline "$fixture" "$scratch/baseline.yaml"
call prepare-config candidate "$fixture" "$scratch/candidate.yaml"
grep -Fq '{{.CommitDate}}' "$scratch/baseline.yaml"
if grep -Fq 'buildmeta.Date={{.Date}}' "$scratch/baseline.yaml"; then exit 1; fi
if grep -Fq GOMAXPROCS "$scratch/baseline.yaml"; then exit 1; fi
test "$(grep -Fc -- '- GOMAXPROCS=1' "$scratch/candidate.yaml")" -eq 1
sed '/      - GOMAXPROCS=1/d' "$scratch/candidate.yaml" >"$scratch/normalized.yaml"
cmp "$scratch/baseline.yaml" "$scratch/normalized.yaml"
test "$(git hash-object --no-filters "$fixture")" = 91dd87214face749e2f061e59bd9ead039720c56
cp "$fixture" "$scratch/drift.yaml"
printf '\n# unreviewed drift\n' >>"$scratch/drift.yaml"
reject call prepare-config baseline "$scratch/drift.yaml" "$scratch/drift-output.yaml"
reject call prepare-config unsupported "$fixture" "$scratch/unsupported.yaml"
reject env GITHUB_TOKEN=experiment-private-sentinel "$helper" prepare-config baseline "$fixture" "$scratch/credentials.yaml"
if grep -Fq experiment-private-sentinel "$scratch/rejected.log"; then exit 1; fi

export GITHUB_RUN_ID=12345 GITHUB_RUN_ATTEMPT=1
export GITHUB_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
mkdir "$scratch/cohort"

# Synthetic reports exercise admission and never claim a measured improvement.
make_report() {
	local pair=$1 arm=$2 elapsed=600 phase=500 child=4
	if [[ "$arm" == candidate ]]; then
		elapsed=450
		phase=360
		child=1
	fi
	jq -n --argjson pair "$pair" --arg arm "$arm" --argjson elapsed "$elapsed" \
		--argjson phase "$phase" --argjson child "$child" '{
		schemaVersion:1,complete:true,pair:$pair,arm:$arm,
		sourceSha:"a3a3957d45f8071c3be2fd2fc96e699459d01d73",tag:"v7.206.65",
		configBlob:"91dd87214face749e2f061e59bd9ead039720c56",
		harnessSha:env.GITHUB_SHA,runId:env.GITHUB_RUN_ID,attempt:env.GITHUB_RUN_ATTEMPT,
		environment:{runnerImage:"ubuntu24",runnerVersion:"frozen-image",processorHash:("a"*64),
			constraintsHash:("e"*64),physicalMemoryBytes:16000000000,hostSwapBytes:4000000000,kernel:"fixed-kernel",
			packagesHash:("b"*64),goVersion:"go1.27.3",goreleaserVersion:"2.19.0",
			outerParallelism:4,defaultChildProcs:4,goMemLimit:"6GiB",nodeVersion:"v24.0.0",npmVersion:"11.0.0",
			limits:{verified:true,memoryBytes:16000000000,swapBytes:null}},
		childProcs:$child,elapsedSeconds:$elapsed,binaryBuildSeconds:$phase,
		resources:{complete:true,scope:"job-cgroup-peak;host-swap;sampled-disk",sampleIntervalSeconds:5,cpuUsageMicros:500000000,memoryPeakBytes:3000000000,minimumFreeDiskBytes:10000000000,
			oomKills:0,swapOutPages:0,sampleCount:10},
		inputs:{uiHash:("c"*64),bundlerHash:("d"*64)},
		nativeCli:{version:true,help:true,schemaFlag:true},
		outputs:(["darwin/arm64","linux/amd64","linux/arm64","windows/amd64","windows/arm64"]
			|map({target:.,sha256:("e"*64),goVersion:"go1.27.3",commit:"a3a3957d45f8071c3be2fd2fc96e699459d01d73",vcsModified:false}))
	}' >"$scratch/cohort/$pair-$arm.json"
}
for pair in 1 2 3; do
	make_report "$pair" baseline
	make_report "$pair" candidate
done
call compare "$scratch/cohort" "$scratch/result.json"
jq -e '.complete==true and .decision=="candidate-qualified" and (.pairs|length)==3' "$scratch/result.json" >/dev/null
cp "$scratch/cohort/1-candidate.json" "$scratch/original.json"

# No failed or incompatible sample may be replaced by a later successful one.
for expression in \
	'.complete=false' \
	'.sourceSha="ffffffffffffffffffffffffffffffffffffffff"' \
	'.runId="99999"' \
	'.attempt="2"' \
	'.harnessSha="ffffffffffffffffffffffffffffffffffffffff"' \
	'.environment.runnerVersion="different-image"' \
	'.environment.processorHash=("f"*64)' \
	'.environment.goVersion="go1.26.9"' \
	'.childProcs=2' \
	'.resources.complete=false' \
	'.resources.oomKills=1' \
	'del(.resources.oomKills)' \
	'del(.resources.swapOutPages)' \
	'del(.environment.hostSwapBytes)' \
	'.outputs|=.[0:4]' \
	'.outputs[0]=.outputs[1]' \
	'.outputs[0].sha256=("f"*64)' \
	'.nativeCli.help=false'; do
	jq "$expression" "$scratch/original.json" >"$scratch/cohort/1-candidate.json"
	reject call compare "$scratch/cohort" "$scratch/rejected-result.json"
done
cp "$scratch/original.json" "$scratch/cohort/1-candidate.json"
mv "$scratch/cohort/3-candidate.json" "$scratch/absent.json"
reject call compare "$scratch/cohort" "$scratch/missing-result.json"
cp "$scratch/absent.json" "$scratch/cohort/3-candidate.json"
printf '{invalid' >"$scratch/cohort/1-candidate.json"
reject call compare "$scratch/cohort" "$scratch/malformed-result.json"
jq '.elapsedSeconds=700|.binaryBuildSeconds=600' "$scratch/original.json" >"$scratch/cohort/1-candidate.json"
call compare "$scratch/cohort" "$scratch/negative.json"
jq -e '.complete==true and .decision=="retain-baseline"' "$scratch/negative.json" >/dev/null
jq '.binaryBuildSeconds=400' "$scratch/original.json" >"$scratch/cohort/1-candidate.json"
call compare "$scratch/cohort" "$scratch/exact-threshold.json"
jq -e '.complete==true and .decision=="candidate-qualified"' "$scratch/exact-threshold.json" >/dev/null
for arm in baseline candidate; do
	cp "$scratch/cohort/2-$arm.json" "$scratch/2-$arm.json"
	jq '.inputs.uiHash=("f"*64)' "$scratch/2-$arm.json" >"$scratch/cohort/2-$arm.json"
done
reject call compare "$scratch/cohort" "$scratch/changed-program.json"
for arm in baseline candidate; do cp "$scratch/2-$arm.json" "$scratch/cohort/2-$arm.json"; done
# A nested target-hook receipt must never masquerade as the binary phase.
printf '2026-11-02T00:00:00Z\t  • building binaries\n2026-11-02T00:01:00Z\t      • took: 1m0s\n2026-11-02T00:01:00Z\t  • writing artifacts metadata\n' >"$scratch/phase.log"
test "$(call phase "$scratch/phase.log")" = 60
sed 's/took: 1m0s/took: 10s/' "$scratch/phase.log" >"$scratch/nested.log"
reject call phase "$scratch/nested.log"
head -n 2 "$scratch/phase.log" >"$scratch/missing-end.log"
reject call phase "$scratch/missing-end.log"
# Verify polling coverage itself, rather than trusting a claimed sample count.
# shellcheck source=.github/scripts/release-build-samples.lib.sh
source "$here/release-build-samples.lib.sh"
# Exercise the collector's actual post-build tail without launching another
# frozen cohort. These files are fixtures, not release artifacts or timings.
awk '/^# Include the actual staged UI / {copy=1} copy {print}' \
	"$here/collect-release-build-experiment.sh" >"$scratch/output-checks.sh"
test -s "$scratch/output-checks.sh"
mkdir -p "$scratch/output-fixture/pkg/webui/dist" "$scratch/output-fixture/dist/linux_amd64"
cat >"$scratch/output-fixture/dist/linux_amd64/ksail" <<'CLI'
#!/usr/bin/env bash
set -euo pipefail
if [[ ${RELEASE_OUTPUT_FAILURE:-} == status ]]; then exit 7; fi
case "$*" in
--version)
	if [[ ${RELEASE_OUTPUT_FAILURE:-} == version ]]; then printf 'wrong-version\n'; else
		printf '7.206.65 a3a3957d45f8071c3be2fd2fc96e699459d01d73 2026-11-02T00:00:00Z\n'
	fi
	;;
--help)
	if [[ ${RELEASE_OUTPUT_FAILURE:-} == help ]]; then printf 'no synopsis\n'; else printf 'Usage\n'; fi
	;;
'workload apply --help')
	if [[ ${RELEASE_OUTPUT_FAILURE:-} == schema ]]; then printf 'no schema option\n'; else printf '%s\n' --schema-location-mode; fi
	;;
*) exit 2 ;;
esac
CLI
chmod +x "$scratch/output-fixture/dist/linux_amd64/ksail"
run_output_checks() {
	local failure=$1 output_report="$scratch/output-report-$1"
	mkdir "$output_report"
	printf '{"complete":false}\n' >"$output_report/report.json"
	printf '60\n' >"$output_report/phase.json"
	printf '[]\n' >"$output_report/outputs.json"
	if [[ $failure == json ]]; then printf '{invalid\n' >"$output_report/outputs.json"; fi
	rm -f "$scratch/output-fixture/pkg/webui/dist/index.html" "$scratch/output-fixture/bundle-1.0.87-5"
	if [[ $failure != ui ]]; then printf 'fixture UI\n' >"$scratch/output-fixture/pkg/webui/dist/index.html"; fi
	for bundle in 1 2 3 4; do touch "$scratch/output-fixture/bundle-1.0.87-$bundle"; done
	if [[ $failure != bundle ]]; then touch "$scratch/output-fixture/bundle-1.0.87-5"; fi
	(
		cd "$scratch/output-fixture"
		export report_dir="$output_report" expected=a3a3957d45f8071c3be2fd2fc96e699459d01d73
		export commit_date=2026-11-02T00:00:00Z RELEASE_OUTPUT_FAILURE="$failure"
		bash -euo pipefail "$scratch/output-checks.sh"
	)
}
run_output_checks success
jq -e '.complete == true and .nativeCli == {version:true,help:true,schemaFlag:true}' \
	"$scratch/output-report-success/report.json" >/dev/null
for failure in ui bundle version help schema json status; do
	output_status=0
	run_output_checks "$failure" >"$scratch/rejected.log" 2>&1 || output_status=$?
	test "$output_status" -ne 0
	if [[ $failure == status ]]; then test "$output_status" -eq 7; fi
	if ! grep -Fxq "release experiment stage outputs failed: status=$output_status" "$scratch/rejected.log"; then
		printf 'FAIL: %s output failure had no stage diagnostic\n' "$failure" >&2
		exit 1
	fi
	jq -e '.complete == false' "$scratch/output-report-$failure/report.json" >/dev/null
done
printf 'PASS: post-build output failures identify their stage and retain incomplete reports\n'
mkdir "$scratch/sampling"
# The caller handles failure explicitly; sample_once must not depend on errexit.
(
	mkdir "$scratch/read-failure-bin"
	printf '#!/bin/sh\nexit 7\n' >"$scratch/read-failure-bin/df"
	chmod +x "$scratch/read-failure-bin/df"
	printf 'Checking disk-read failure propagation under a conditional caller\n'
	PATH=$scratch/read-failure-bin:$PATH GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch \
		reject sample_once "$scratch" "$scratch/sampling"
)
reject release_stage build bash -c 'exit 7'
grep -Fq 'stage build failed: status=7' "$scratch/rejected.log"
# Exercise the actual sampler function while its real sample_once is blocked
# in a controlled df. Sending TERM before releasing that read reproduces the
# shutdown race; no frozen build, cloud call or process-wide signal is used.
awk '/^sample_disk\(\) \{/ {copy=1} copy {print} copy && /^}/ {exit}' \
	"$here/collect-release-build-experiment.sh" >"$scratch/actual-sampler.sh"
test -s "$scratch/actual-sampler.sh"
mkdir "$scratch/race-bin"
cat >"$scratch/race-bin/df" <<'DF'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 5 && $1 == -Pk ]]
printf 'ready\n' >"${SAMPLER_READY:?}"
IFS= read -r action <"${SAMPLER_RELEASE:?}"
case "$action" in
7 | 143) exit "$action" ;;
valid)
	printf 'Filesystem 1024-blocks Used Available Capacity Mounted\n'
	for row in 1 2 3 4; do printf 'fixture 100 0 100 0%% /\n'; done
	;;
*) exit 2 ;;
esac
DF
cat >"$scratch/race-bin/date" <<'DATE'
#!/usr/bin/env bash
[[ $# == 1 && $1 == +%s.%N ]] || exit 2
printf '100.000000000\n'
DATE
# The collector is Linux-only; emulate GNU wc's unpadded stdin count on macOS.
cat >"$scratch/race-bin/wc" <<'WC'
#!/usr/bin/env bash
[[ $# == 1 && $1 == -l ]] || exit 2
/usr/bin/wc -l | tr -d '[:space:]'
WC
chmod +x "$scratch/race-bin/df" "$scratch/race-bin/date" "$scratch/race-bin/wc"
cat >"$scratch/sampler-race.sh" <<'RACE'
#!/usr/bin/env bash
set -euo pipefail
source "$1"
source "$2"
source_dir=$3
report_dir=$4
action=$5
export GOCACHE="$source_dir" GOMODCACHE="$source_dir" GOTMPDIR="$source_dir"
export SAMPLER_READY="$report_dir/ready" SAMPLER_RELEASE="$report_dir/release"
mkfifo "$SAMPLER_READY" "$SAMPLER_RELEASE"
exec 9<>"$SAMPLER_READY"
sampler=
trap 'if [[ -n $sampler ]]; then builtin kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true; fi' EXIT
# Keep the helper's actual kill/wait path, but release the reader only after
# the real TERM has been sent. This avoids timing-based sleeps in the control.
kill() {
	builtin kill "$@" || return
	if [[ $1 != -0 ]]; then printf '%s\n' "$action" >"$SAMPLER_RELEASE"; fi
}
sample_disk &
sampler=$!
ready=
IFS= read -r -t 10 ready <&9
[[ $ready == ready ]]
collection_status=0
finish_release_sampler "$sampler" || collection_status=$?
sampler=
printf 'collection_status=%s\n' "$collection_status"
RACE
for mode in valid 7 143 append; do
	race_dir="$scratch/sampler-race-$mode"
	mkdir "$race_dir"
	read_result=$mode
	if [[ $mode == append ]]; then
		read_result=valid
		mkdir "$race_dir/disk-samples"
	fi
	if ! PATH="$scratch/race-bin:$PATH" bash "$scratch/sampler-race.sh" \
		"$here/release-build-samples.lib.sh" "$scratch/actual-sampler.sh" \
		"$scratch" "$race_dir" "$read_result" >"$race_dir/result" 2>&1; then
		cat "$race_dir/result" >&2
		printf 'FAIL: actual sampler control %s did not complete\n' "$mode" >&2
		exit 1
	fi
	if [[ $mode == valid ]]; then
		if ! grep -Fxq collection_status=0 "$race_dir/result"; then
			cat "$race_dir/result" >&2
			exit 1
		fi
		test "$(wc -l <"$race_dir/disk-samples")" -eq 1
	else
		if ! grep -Fxq collection_status=1 "$race_dir/result"; then
			cat "$race_dir/result" >&2
			printf 'FAIL: actual sampler control %s accepted failure\n' "$mode" >&2
			exit 1
		fi
		observation_status=$mode
		if [[ $mode == append ]]; then observation_status=1; fi
		grep -Fxq "release experiment observation failed: status=$observation_status" "$race_dir/result"
	fi
done
printf 'PASS: actual in-flight sampler read/append failures survive TERM, including status 143\n'
bash "$here/release-build-collector.test.sh"
for stage in phase outputs source; do
	reject release_stage "$stage" false
	grep -Fq "stage $stage failed: status=1" "$scratch/rejected.log"
done
if [[ $(uname -s) == Linux ]]; then
	GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch sample_once "$scratch" "$scratch/sampling"
	test "$(wc -l <"$scratch/sampling/disk-samples")" -eq 1
	awk 'NF!=2 || $1 !~ /^[0-9]+[.][0-9]+$/ || $2 !~ /^[0-9]+$/ {exit 1}' "$scratch/sampling/disk-samples"
	(
		native_sampler=
		trap 'if [[ -n $native_sampler ]]; then kill "$native_sampler" 2>/dev/null || true; wait "$native_sampler" 2>/dev/null || true; fi' EXIT
		mkdir "$scratch/live-sampling" "$scratch/full-sampling"
		mkfifo "$scratch/sampler-ready"
		exec 9<>"$scratch/sampler-ready"
		sample_loop() {
			local samples=0
			while :; do
				sample_once "$scratch" "$scratch/live-sampling" || return
				samples=$((samples + 1))
				if [[ $samples == 2 ]]; then printf '%s\n' sampled >&9; fi
				sleep 5
			done
		}
		started=$(date +%s.%N)
		GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch sample_loop &
		native_sampler=$!
		sampler_ready=
		IFS= read -r -t 20 sampler_ready <&9
		test "$sampler_ready" = sampled
		exec 9>&-
		finished=$(date +%s.%N)
		native_status=0
		finish_release_sampler "$native_sampler" || native_status=$?
		native_sampler=
		test "$native_status" -eq 0
		GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch sample_once "$scratch" "$scratch/live-sampling"
		validate_disk_samples "$scratch/live-sampling/disk-samples" "$started" "$finished"

		(exit 9) &
		native_sampler=$!
		wait "$native_sampler" || true
		native_status=0
		finish_release_sampler "$native_sampler" >"$scratch/early-sampler.log" 2>&1 || native_status=$?
		native_sampler=
		test "$native_status" -ne 0
		grep -Fq 'sampler_status=9' "$scratch/early-sampler.log"

		ln -s /dev/full "$scratch/full-sampling/disk-samples"
		GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch sample_once "$scratch" "$scratch/full-sampling" &
		native_sampler=$!
		wait "$native_sampler" || true
		native_status=0
		finish_release_sampler "$native_sampler" >"$scratch/full-sampler.log" 2>&1 || native_status=$?
		native_sampler=
		test "$native_status" -ne 0
		grep -Fq 'sampler_status=1' "$scratch/full-sampler.log"
		printf 'PASS: native Linux sampler collection, early exit and failed append\n'
	)
fi
printf '100 10000000000\n105 9000000000\n110 9000000000\n' >"$scratch/disk-samples"
validate_disk_samples "$scratch/disk-samples" 100 110
printf '100 10000000000\n' >"$scratch/disk-samples"
reject validate_disk_samples "$scratch/disk-samples" 100 110
printf '100 10000000000\n130 9000000000\n' >"$scratch/disk-samples"
reject validate_disk_samples "$scratch/disk-samples" 100 130
printf '100 10000000000\n105 9000000000\n' >"$scratch/disk-samples"
reject validate_disk_samples "$scratch/disk-samples" 100 130
printf 'PASS: frozen release configuration and adversarial comparison cohort\n'
