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
		sample_loop() {
			while :; do
				sample_once "$scratch" "$scratch/live-sampling" || return
				sleep 5
			done
		}
		started=$(date +%s.%N)
		GOCACHE=$scratch GOMODCACHE=$scratch GOTMPDIR=$scratch sample_loop &
		native_sampler=$!
		sleep 6
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
