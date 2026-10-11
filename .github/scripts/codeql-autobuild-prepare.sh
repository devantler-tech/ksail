#!/usr/bin/env bash
# Give a GitHub-hosted runner room for CodeQL's Go extraction, and log what memory it has.
#
# CodeQL's Go autobuilder runs plain `make` at the repository root before it extracts the Go
# code, and the Makefile routes that call here. It is the one place the repository can act
# inside GitHub's managed Code Quality job, which takes no workflow file, environment or
# configuration from the repository (#7131).
#
# The root extraction needs more than 13 GiB while the Go heap is untuned, on a runner with
# 16 GB, and the managed job keeps ending with a runner shutdown part-way through it. So this
# script:
#   1. adds swap, so a peak above physical memory is paged out instead of starving the runner;
#   2. writes memory, swap and the largest process to the job log every few seconds until the
#      autobuilder exits, so a run that ends early shows what the runner had left.
# It changes nothing about what is extracted or analysed, and it never fails the build.
#
# Usage: codeql-autobuild-prepare.sh MAKE_PID
set -uo pipefail
export LC_ALL=C

readonly prefix='ksail-codeql-memory:'
proc_root="${KSAIL_CODEQL_PROC_ROOT:-/proc}"
swap_gib="${KSAIL_CODEQL_SWAP_GIB:-8}"
interval="${KSAIL_CODEQL_SAMPLE_SECONDS:-5}"
max_seconds="${KSAIL_CODEQL_SAMPLE_MAX_SECONDS:-7200}"

# meminfo_mib prints one /proc/meminfo field in MiB, or 0 when it is missing.
meminfo_mib() {
	awk -v field="$1:" '$1 == field { printf "%d", $2 / 1024; found = 1 } END { if (!found) printf "0" }' \
		"$proc_root/meminfo" 2>/dev/null || printf '0'
}

# memory_line prints the runner's memory, swap and largest process on one line.
memory_line() {
	local swap_total swap_free top
	swap_total="$(meminfo_mib SwapTotal)"
	swap_free="$(meminfo_mib SwapFree)"
	top="$(ps -eo rss=,comm= --sort=-rss 2>/dev/null | awk 'NR == 1 { printf "%s=%dMiB", $2, $1 / 1024 }')"
	printf '%s %s mem_total=%sMiB mem_available=%sMiB swap_total=%sMiB swap_used=%sMiB largest=%s\n' \
		"$prefix" "$1" "$(meminfo_mib MemTotal)" "$(meminfo_mib MemAvailable)" \
		"$swap_total" "$((swap_total - swap_free))" "${top:-unknown}"
}

# free_gib prints the whole GiB available on the filesystem holding a directory.
free_gib() {
	df -BG --output=avail "$1" 2>/dev/null | awk 'NR == 2 { gsub(/[^0-9]/, ""); print }'
}

# add_swap creates and enables one swap file, on the first disk with room to spare.
add_swap() {
	local directory margin available file
	for directory in /mnt /; do
		# Nothing else writes to /mnt; the root disk also holds the module cache and database.
		if [[ "$directory" == /mnt ]]; then margin=10; else margin=20; fi
		available="$(free_gib "$directory")"
		[[ "$available" =~ ^[0-9]+$ ]] || continue
		((available >= swap_gib + margin)) || continue
		file="${directory%/}/ksail-codeql.swap"
		if sudo -n fallocate -l "${swap_gib}G" "$file" && sudo -n chmod 600 "$file" &&
			sudo -n mkswap "$file" >/dev/null && sudo -n swapon "$file"; then
			printf '%s added %s GiB of swap at %s (%s GiB were free there)\n' \
				"$prefix" "$swap_gib" "$file" "$available"
			return 0
		fi
		printf '%s could not enable swap at %s\n' "$prefix" "$file"
		sudo -n rm -f "$file" 2>/dev/null
		return 0
	done
	printf '%s no disk has room for %s GiB of swap; none added\n' "$prefix" "$swap_gib"
}

# sample_until_exit appends a memory line to a process's own error stream until it exits.
# It opens that stream for one line at a time, so it never keeps the job's log open.
sample_until_exit() {
	local watched="$1" start=$SECONDS
	while kill -0 "$watched" 2>/dev/null && ((SECONDS - start < max_seconds)); do
		memory_line "t=+$((SECONDS - start))s" >>"$proc_root/$watched/fd/2" 2>/dev/null || return 0
		sleep "$interval"
	done
}

if [[ "$(uname -s)" != Linux || "${GITHUB_ACTIONS:-}" != true || "${RUNNER_ENVIRONMENT:-}" != github-hosted ]]; then
	printf '%s not a GitHub-hosted Linux runner; nothing to do\n' "$prefix"
	exit 0
fi

# The autobuilder may call `make` once per Go module; prepare the runner only once per job.
state="${RUNNER_TEMP:-/tmp}/ksail-codeql-autobuild"
if ! mkdir "$state" 2>/dev/null; then
	if [[ -d "$state" ]]; then
		printf '%s runner already prepared in this job\n' "$prefix"
	else
		printf '%s cannot create %s; nothing done\n' "$prefix" "$state"
	fi
	exit 0
fi

memory_line before
add_swap
memory_line after

make_pid="${1:-}"
watched=""
if [[ "$make_pid" =~ ^[0-9]+$ ]]; then
	watched="$(ps -o ppid= -p "$make_pid" 2>/dev/null | tr -d '[:space:]')"
fi
if [[ "$watched" =~ ^[0-9]+$ && "$watched" -gt 1 && -w "$proc_root/$watched/fd/2" ]]; then
	printf '%s sampling every %ss until process %s exits\n' "$prefix" "$interval" "$watched"
	(
		exec </dev/null >/dev/null 2>&1
		sample_until_exit "$watched"
	) &
	disown
else
	printf '%s cannot write to the calling process; no samples will follow\n' "$prefix"
fi
exit 0
