#!/usr/bin/env bash
# Shared only by the fixed collector and its offline regression tests.
sample_once() {
	local source_dir=$1 report_dir=$2 free timestamp
	free=$(df -Pk "$source_dir" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" |
		awk 'NR>1 {if (NF<4 || $4 !~ /^[0-9]+$/) exit 1; printf "%.0f\n",$4*1024}') || return
	[[ $(wc -l <<<"$free") == 4 ]] || return 1
	free=$(sort -n <<<"$free" | head -n 1) || return
	[[ $free =~ ^[0-9]+$ ]] || return 1
	timestamp=$(date +%s.%N) || return
	[[ $timestamp =~ ^[0-9]+[.][0-9]+$ ]] || return 1
	printf '%s %s\n' "$timestamp" "$free" >>"$report_dir/disk-samples"
}

# A background sampler can have exited before collection. Always join it and
# retain its actual status; a failed liveness probe must not hide the build.
finish_release_sampler() {
	local sampler_pid=$1 sampler_status=0 stopped=0
	[[ $sampler_pid =~ ^[1-9][0-9]*$ ]] || return 2
	if kill -0 "$sampler_pid" 2>/dev/null; then
		if kill "$sampler_pid" 2>/dev/null; then stopped=1; fi
	fi
	wait "$sampler_pid" || sampler_status=$?
	if [[ $stopped == 1 && $sampler_status == 143 ]]; then return 0; fi
	printf 'release experiment sampler failed: sampler_status=%s stop_sent=%s\n' "$sampler_status" "$stopped" >&2
	return 1
}

# Only fixed stage names and statuses reach diagnostics. Call with external
# commands or shell builtins, not shell functions that depend on errexit.
release_stage() {
	local stage=$1 status
	shift
	case "$stage" in build | phase | outputs | source) ;; *) return 2 ;; esac
	if "$@"; then return 0; else status=$?; fi
	printf 'release experiment stage %s failed: status=%s\n' "$stage" "$status" >&2
	return "$status"
}

validate_disk_samples() {
	awk -v start="$2" -v end="$3" '
    NR==1 {if ($1-start>10) exit 1; previous=$1}
    {if (NF!=2 || $2 !~ /^[0-9]+$/ || $1<previous || $1-previous>15) exit 1; previous=$1}
    END {if (NR<2 || end-previous>10) exit 1}' "$1"
}
