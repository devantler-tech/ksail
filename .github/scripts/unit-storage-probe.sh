#!/usr/bin/env bash
# KSail #8153: observe the failed unit workload without compiling a collector or
# warming its Go cache. Private paths/output never enter the published receipt.
set -euo pipefail
umask 077

die() {
	printf '%s\n' "$1" >&2
	exit 2
}
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted &&
	"${GITHUB_REPOSITORY:-}" == devantler-tech/ksail && "${GITHUB_REF:-}" == refs/heads/main &&
	"${RUNNER_OS:-}" == Linux ]] || die 'unit storage probe requires GitHub-hosted main'
[[ "${RUNNER_TEMP:-}" == /* && "${GITHUB_WORKSPACE:-}" == /* ]] || die 'invalid probe workspace'
[[ "${GITHUB_SHA:-}" =~ ^[a-f0-9]{40}$ && "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ &&
	"${GITHUB_RUN_ATTEMPT:-}" =~ ^[1-9][0-9]*$ ]] || die 'invalid probe identity'

consumer="$GITHUB_WORKSPACE/.unit-consumer"
state="$RUNNER_TEMP/unit-storage-probe"
source_sha=bab0963e3c16b91e8a236e9679a29ffcde276b8f
cache_key=go-mod-v1-Linux-X64-.-7c4839f70b57f520b6effc59074c3d5bd9199ccbc893762f484f974b97f58853
timeout_command=timeout
if command -v gnutimeout >/dev/null 2>&1; then timeout_command=gnutimeout; fi
command -v "$timeout_command" >/dev/null || die 'bounded observation requires GNU timeout'
[[ ! -L "$state" ]] || die 'invalid probe state'
mkdir -p "$state/public"
chmod 700 "$state" "$state/public"

verify_source() {
	[[ "$(git -C "$consumer" rev-parse HEAD)" == "$source_sha" ]] || die 'unexpected control source'
	git -C "$consumer" diff --quiet
	git -C "$consumer" diff --cached --quiet
}

prepare() {
	verify_source
	(
		cd "$consumer"
		go env -json GOCACHE GOMODCACHE GOROOT GOVERSION GOTOOLCHAIN CGO_ENABLED GOFLAGS GOTMPDIR
	) >"$state/go-env.json"
	jq -e '.GOVERSION == "go1.27.3" and .GOTOOLCHAIN == "local" and .CGO_ENABLED == "1" and
    .GOFLAGS == "" and .GOTMPDIR == "" and
    ([.GOCACHE,.GOMODCACHE,.GOROOT] | all(type == "string" and startswith("/") and (test("[\\r\\n]")|not)))' \
		"$state/go-env.json" >/dev/null || die 'unexpected control Go environment'
	[[ "${TMPDIR:-/tmp}" == /tmp ]] || die 'unexpected control temporary storage'
	local build_cache cache_entry
	build_cache="$(jq -r .GOCACHE "$state/go-env.json")"
	mkdir -p "$build_cache"
	cache_entry="$(find "$build_cache" -mindepth 1 ! -name README ! -name trim.txt -print -quit 2>/dev/null)" ||
		die 'compilation cache could not be examined'
	[[ -z "$cache_entry" ]] ||
		die 'compilation cache is not controlled cold'
	touch "$state/prepared"
	if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
		printf 'module-cache=%s\n' "$(jq -r .GOMODCACHE "$state/go-env.json")" >>"$GITHUB_OUTPUT"
	fi
}

# Inspect each pathname separately: equal device IDs do not prove equal
# per-directory quota behavior. Only opaque group IDs and numeric counters leave.
sample_path() {
	local label="$1" path="$2" components="$3" bytes inodes device group size=null size_status=not-collected
	if [[ -z "$path" ]]; then
		jq -nc --arg label "$label" '{label:$label,status:"not-yet-resolved"}'
		return
	fi
	if ! bytes="$("$timeout_command" --kill-after=1s 5s env LC_ALL=C df -Pk "$path" 2>/dev/null |
		awk 'NR==2 && NF==6 {print $2,$3,$4} END {if(NR!=2) exit 1}')" ||
		! inodes="$("$timeout_command" --kill-after=1s 5s env LC_ALL=C df -Pi "$path" 2>/dev/null |
			awk 'NR==2 && NF==6 {print $2,$3,$4} END {if(NR!=2) exit 1}')" ||
		! device="$("$timeout_command" --kill-after=1s 5s stat -L -c '%d' -- "$path" 2>/dev/null)" ||
		[[ ! "$bytes" =~ ^[0-9]{1,12}\ [0-9]{1,12}\ [0-9]{1,12}$ ||
			! "$inodes" =~ ^[0-9]{1,12}\ [0-9]{1,12}\ [0-9]{1,12}$ || ! "$device" =~ ^[0-9]{1,20}$ ]]; then
		jq -nc --arg label "$label" '{label:$label,status:"unknown"}'
		return
	fi
	touch "$state/devices"
	group="$(awk -v device="$device" '$0==device {print NR; found=1; exit} END {if(!found) exit 1}' "$state/devices")" || {
		printf '%s\n' "$device" >>"$state/devices"
		group="$(wc -l <"$state/devices" | tr -d ' ')"
	}
	if [[ "$components" == true && "$label" != root ]]; then
		size_status=unknown
		if size="$("$timeout_command" --kill-after=1s 10s du -sHk -- "$path" 2>/dev/null | awk 'NR==1 {print $1} END {if(NR!=1) exit 1}')" &&
			[[ "$size" =~ ^[0-9]{1,12}$ ]]; then size_status=measured; else size=null; fi
	fi
	jq -nc --arg label "$label" --arg group "fs-$group" --arg bytes "$bytes" --arg inodes "$inodes" \
		--argjson size "$size" --arg size_status "$size_status" '
    ($bytes|split(" ")|map(tonumber)) as $b | ($inodes|split(" ")|map(tonumber)) as $i |
    if ($b[0]>0 and $b[1]+$b[2]<=$b[0] and $i[0]>0 and $i[1]+$i[2]<=$i[0]) then
      {label:$label,status:"measured",filesystemGroup:$group,totalKiB:$b[0],usedKiB:$b[1],availableKiB:$b[2],
       totalInodes:$i[0],usedInodes:$i[1],availableInodes:$i[2],componentKiB:$size,componentStatus:$size_status}
    else {label:$label,status:"unknown"} end'
}

snapshot() {
	local phase="$1" components="$2" module_cache='' build_cache='' toolchain='' index
	case "$phase" in before-cleanup | after-cleanup | after-setup | after-restore | before-test | during-test | after-test) ;; *) die 'invalid snapshot phase' ;; esac
	if [[ -f "$state/go-env.json" ]]; then
		module_cache="$(jq -r .GOMODCACHE "$state/go-env.json")"
		build_cache="$(jq -r .GOCACHE "$state/go-env.json")"
		toolchain="$(jq -r .GOROOT "$state/go-env.json")"
	fi
	local labels=(root workspace go-temp runner-temp module-cache build-cache toolchain)
	local paths=(/ "$consumer" /tmp "$RUNNER_TEMP" "$module_cache" "$build_cache" "$toolchain")
	: >"$state/rows.jsonl"
	for index in "${!labels[@]}"; do
		sample_path "${labels[$index]}" "${paths[$index]}" "$components" >>"$state/rows.jsonl"
	done
	jq -nc --arg phase "$phase" --argjson epoch "$(date +%s)" --slurpfile entries "$state/rows.jsonl" \
		'{phase:$phase,epoch:$epoch,entries:$entries}' >>"$state/frames.jsonl"
	[[ "$(wc -c <"$state/frames.jsonl")" -le 8388608 ]] || die 'observation size budget exceeded'
}

run_control() {
	verify_source
	[[ "${UNIT_STORAGE_CACHE_HIT:-}" == true && "${UNIT_STORAGE_CACHE_KEY:-}" == "$cache_key" ]] ||
		die 'exact control cache unavailable; workload not examined'
	[[ -f "$state/go-env.json" ]] || die 'control toolchain was not prepared'
	touch "$state/cache-verified"
	snapshot before-test true
	local sampler_pid='' command_pid='' completed=false result=2
	finish_run() {
		local exit_status=$?
		trap - EXIT INT TERM
		if [[ -n "$sampler_pid" ]]; then
			kill "$sampler_pid" 2>/dev/null || true
			wait "$sampler_pid" 2>/dev/null || true
		fi
		if [[ "$completed" != true ]]; then
			if [[ -n "$command_pid" ]]; then
				kill "$command_pid" 2>/dev/null || true
				wait "$command_pid" 2>/dev/null || true
			fi
			jq -nc --argjson status "$exit_status" '{executed:true,completed:false,exit:$status}' >"$state/command.json"
		fi
	}
	trap finish_run EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	(
		trap 'exit 0' INT TERM
		for ((sample = 0; sample < 1800; sample++)); do
			components=false
			if ((sample % 60 == 0)); then components=true; fi
			if ! snapshot during-test "$components"; then
				touch "$state/observation-incomplete"
				exit 2
			fi
			sleep 1
		done
		touch "$state/observation-incomplete"
	) &
	sampler_pid=$!
	(
		cd "$consumer"
		exec "$timeout_command" --kill-after=15s 20m go test ./...
	) >"$state/private-test.log" 2>&1 &
	command_pid=$!
	set +e
	wait "$command_pid"
	result=$?
	set -e
	completed=true
	kill "$sampler_pid" 2>/dev/null || true
	wait "$sampler_pid" 2>/dev/null || true
	sampler_pid=''
	snapshot after-test true
	jq -nc --argjson result "$result" '{executed:true,completed:true,exit:$result}' >"$state/command.json"
	printf 'UNIT_STORAGE_COMMAND exit=%s\n' "$result"
	trap - EXIT INT TERM
	return "$result"
}

publish() {
	local command_file="$state/command.json"
	local prepared=false cache_verified=false incomplete=false
	[[ ! -f "$state/prepared" ]] || prepared=true
	[[ ! -f "$state/cache-verified" ]] || cache_verified=true
	[[ ! -f "$state/observation-incomplete" ]] || incomplete=true
	if [[ ! -f "$command_file" ]]; then
		jq -nc '{executed:false,completed:false,exit:null}' >"$command_file"
	fi
	[[ -f "$state/frames.jsonl" && "$(wc -c <"$state/frames.jsonl")" -le 8388608 ]] || die 'storage observation missing or oversized'
	jq -nc --arg source "$source_sha" --arg harness "$GITHUB_SHA" --arg run "$GITHUB_RUN_ID" \
		--arg attempt "$GITHUB_RUN_ATTEMPT" --arg image "${ImageOS:-unknown}" --arg image_version "${ImageVersion:-unknown}" \
		--arg key "$cache_key" --argjson prepared "$prepared" --argjson cache_verified "$cache_verified" \
		--argjson incomplete "$incomplete" --slurpfile frames "$state/frames.jsonl" --slurpfile command "$command_file" \
		'{schema:1,source:$source,harness:$harness,run:$run,attempt:$attempt,runnerImage:$image,runnerImageVersion:$image_version,
      expectedGo:"go1.27.3",actualGo:(if $prepared then "go1.27.3" else null end),
      expectedModuleCacheKey:$key,moduleCacheVerified:$cache_verified,
      compilationCache:(if $prepared then "verified-empty" else "unverified" end),schemaCache:"not-restored",
      quotaLimits:"not-established",command:$command[0],frames:$frames,
      countersComplete:($prepared and $cache_verified and ($incomplete|not) and
        (["before-cleanup","after-cleanup","after-setup","after-restore","before-test","during-test","after-test"] | all(. as $phase |
          any($frames[]; .phase == $phase))) and
        all($frames[];
          (.entries|length)==7 and
          if (.phase=="before-cleanup" or .phase=="after-cleanup") then
            all(.entries[]; if (.label=="module-cache" or .label=="build-cache" or .label=="toolchain")
              then .status=="not-yet-resolved" else .status=="measured" end)
          else all(.entries[]; .status=="measured") end)),
      componentsComplete:($prepared and $cache_verified and
        all($frames[] | select(.phase=="after-setup" or .phase=="after-restore" or .phase=="before-test" or .phase=="after-test");
          (.entries|length)==7 and all(.entries[] | select(.label!="root"); .componentStatus=="measured"))),
      limitations:["Sampled counters are not instantaneous peaks.","Component walks can delay samples.","Per-user and project quota limits are not established."]}' \
		>"$state/receipt.json"
	[[ "$(wc -c <"$state/receipt.json")" -le 8388608 ]] || die 'storage receipt oversized'
	mv "$state/receipt.json" "$state/public/report.json"
	jq -e '.countersComplete and .componentsComplete' "$state/public/report.json" >/dev/null || die 'storage observations are incomplete'
}

case "${1:-}" in
prepare) prepare ;;
snapshot) snapshot "${2:-}" true ;;
run) run_control ;;
publish) publish ;;
*) die 'invalid unit storage probe operation' ;;
esac
