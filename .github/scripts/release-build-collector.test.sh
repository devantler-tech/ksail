#!/usr/bin/env bash
# Run the real collector against synthetic tools, never a release cohort.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
scratch=$(mktemp -d)
scratch=$(cd "$scratch" && pwd -P)
trap 'rm -rf "$scratch"' EXIT

# The collector must preserve both failures, and a healthy sampler must not
# replace the measured build's exit 7 with the result of a Boolean comparison.
bash_bin=$(command -v bash)
timeout_bin=$(command -v timeout)
realpath_bin=$(command -v realpath)
awk_bin=$(command -v awk)
grep_bin=$(command -v grep)
wc_bin=$(command -v wc)
git_bin=$(command -v git)
tool_path=$PATH
mkdir "$scratch/bin"
cat >"$scratch/bin/fixture-tool" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
tool=${0##*/}
unexpected() {
	printf '%s: %s\n' "$tool" "$*" >>"$CASE_DIR/unexpected"
	exit 97
}
case "$tool" in
git)
	case "$*" in
	'rev-parse HEAD' | 'rev-parse v7.206.65^{commit}') printf '%s\n' a3a3957d45f8071c3be2fd2fc96e699459d01d73 ;;
	'status --porcelain --untracked-files=all') ;;
	'hash-object --no-filters .goreleaser.yaml') exec "$REAL_GIT" "$@" ;;
	*) unexpected "$@" ;;
	esac
	;;
go)
	[[ $* == 'env GOVERSION' ]] || unexpected "$@"
	printf 'go1.27.3\n'
	;;
goreleaser)
	if [[ $* == --version ]]; then printf 'GitVersion: 2.19.0\n'; exit 0; fi
	[[ $# == 8 && $1 == build && $2 == --clean && $3 == --config &&
		$4 == "$CASE_DIR/report/config.yaml" && $5 == --parallelism && $6 == 4 &&
		$7 == --timeout && $8 == 55m ]] || unexpected "$@"
	printf 'started\n' >"$CASE_DIR/build-started"
	# The first df is in flight before the build ends. Both FIFOs have an
	# inherited reader/writer, and every read is bounded independently.
	IFS= read -r -t 3 ready <&8 || unexpected 'sampler did not start'
	[[ $ready == sampling ]] || unexpected 'invalid sampler handshake'
	printf 'finish\n' >&9
	exit 7
	;;
df)
	[[ $# == 5 && $1 == -Pk && $2 == "$CASE_DIR/source" &&
		$3 == "$GOCACHE" && $4 == "$GOMODCACHE" && $5 == "$GOTMPDIR" ]] || unexpected "$@"
	if [[ ! -e $CASE_DIR/df-started ]]; then
		printf 'started\n' >"$CASE_DIR/df-started"
		printf 'sampling\n' >&8
		IFS= read -r -t 3 finish <&9 || unexpected 'build did not release sampler'
		[[ $finish == finish ]] || unexpected 'invalid build handshake'
	fi
	printf 'Filesystem 1024-blocks Used Available Capacity Mounted\n'
	for number in 1 2 3 4; do printf 'fixture%s 20000000 1 19000000 1%% /fixture\n' "$number"; done
	;;
date)
	[[ $* == +%s.%N ]] || unexpected "$@"
	printf '1700000000.000000000\n'
	;;
realpath)
	[[ $# == 1 ]] || unexpected "$@"
	case "$1" in
	/sys/fs/cgroup/) printf '/sys/fs/cgroup\n' ;;
	"$CASE_DIR/helper" | "$CASE_DIR/source" | "$CASE_DIR/report") exec "$REAL_REALPATH" "$@" ;;
	*) unexpected "$@" ;;
	esac
	;;
awk)
	case "${*: -1}" in
	/proc/self/cgroup)
		[[ $# == 3 && $1 == -F: && $2 == '$1=="0" && $2=="" {print $3}' ]] || unexpected "$@"
		printf '/\n'
		;;
	/sys/fs/cgroup/memory.events | /sys/fs/cgroup/cpu.stat | /proc/vmstat)
		[[ $# == 4 && $1 == -v && $3 == '$1==key {print $2}' ]] || unexpected "$@"
		case "$2:${*: -1}" in
		key=oom_kill:/sys/fs/cgroup/memory.events | key=pswpout:/proc/vmstat) printf '0\n' ;;
		key=usage_usec:/sys/fs/cgroup/cpu.stat) printf '1000\n' ;;
		*) unexpected "$@" ;;
		esac
		;;
	/proc/cpuinfo)
		[[ $# == 3 && $1 == -F: && $2 == '/^(vendor_id|model name|flags)[[:space:]]*:/ {print}' ]] || unexpected "$@"
		printf 'model name: synthetic collector fixture\n'
		;;
	/proc/meminfo)
		[[ $# == 2 ]] || unexpected "$@"
		case "$1" in
		'$1=="MemTotal:" {printf "%.0f",$2*1024}') printf '16000000000\n' ;;
		'$1=="SwapTotal:" {printf "%.0f",$2*1024}') printf '0\n' ;;
		*) unexpected "$@" ;;
		esac
		;;
	*)
		if [[ $# == 1 && $1 == 'NR>1 {if (NF<4 || $4 !~ /^[0-9]+$/) exit 1; printf "%.0f\n",$4*1024}' ]] ||
			[[ $# == 2 && $1 == '{print $2}' && $2 == "$CASE_DIR/report/disk-samples" ]] ||
			[[ $# == 5 && $1 == -v && $2 == start=* && $3 == -v && $4 == end=* && $5 == 'BEGIN {print end-start}' ]] ||
			[[ $# == 6 && $1 == -v && $2 == start=* && $3 == -v && $4 == end=* &&
				$5 == *'NR==1 {if ($1-start>10) exit 1; previous=$1}'* && $6 == "$CASE_DIR/report/disk-samples" ]]; then
			exec "$REAL_AWK" "$@"
		fi
		unexpected "$@"
		;;
	 esac
	;;
cat)
	[[ $# == 1 ]] || unexpected "$@"
	case "$1" in
	/sys/fs/cgroup/memory.peak) printf '1000000000\n' ;;
	/sys/fs/cgroup/cpu.max) printf 'max 100000\n' ;;
	/sys/fs/cgroup/cpuset.cpus.effective | /sys/fs/cgroup/cpuset.mems.effective) printf '0\n' ;;
	*) unexpected "$@" ;;
	esac
	;;
grep)
	if [[ $# == 3 && $1 == -E && $2 == '^(Cpus_allowed_list|Mems_allowed_list):' && $3 == /proc/self/status ]]; then
		printf 'Cpus_allowed_list: 0\nMems_allowed_list: 0\n'
	elif [[ $# == 2 && $1 == -Eq && $2 == '^GitVersion:[[:space:]]+2\.19\.0$' ]]; then
		exec "$REAL_GREP" "$@"
	else unexpected "$@"; fi
	;;
wc)
	[[ $# == 1 && $1 == -l ]] || unexpected "$@"
	# GNU wc's stdin count has no padding; retain its real count on macOS too.
	count=$("$REAL_WC" -l)
	printf '%s\n' "$((count))"
	;;
dpkg-query)
	[[ $# == 2 && $1 == -W && $2 == '-f=${Package}=${Version}\n' ]] || unexpected "$@"
	printf 'synthetic-package=1\n'
	;;
node) [[ $* == --version ]] || unexpected "$@"; printf 'v24.0.0\n' ;;
npm) [[ $* == --version ]] || unexpected "$@"; printf '11.0.0\n' ;;
uname) [[ $* == -r ]] || unexpected "$@"; printf 'synthetic-kernel\n' ;;
*) unexpected "$@" ;;
esac
STUB
chmod +x "$scratch/bin/fixture-tool"
for tool in git go goreleaser df date realpath awk cat grep wc dpkg-query node npm uname; do
	ln -s fixture-tool "$scratch/bin/$tool"
done

cat >"$scratch/helper" <<'HELPER'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
runtime) printf '{"goVersion":"go1.27.3","defaultChildProcs":2}\n' ;;
limits) printf '{}\n' ;;
"prepare-config baseline .goreleaser.yaml $CASE_DIR/report/config.yaml") printf 'synthetic-config\n' >"$CASE_DIR/report/config.yaml" ;;
*)
	printf '%s\n' "$*" >"$CASE_DIR/forbidden-helper-stage"
	exit 97
	;;
esac
HELPER
chmod +x "$scratch/helper"

failures=0
for mode in combined build-only; do
	case_dir="$scratch/$mode"
	mkdir -p "$case_dir/source" "$case_dir/report" "$case_dir/home" \
		"$case_dir/cache" "$case_dir/modules" "$case_dir/tmp" "$case_dir/tools-tmp"
	cp "$scratch/helper" "$case_dir/helper"
	cp "$here/../tests/fixtures/release-build-experiment/goreleaser.yaml" "$case_dir/source/.goreleaser.yaml"
	if [[ $mode == combined ]]; then
		if [[ -c /dev/full ]]; then ln -s /dev/full "$case_dir/report/disk-samples"; else
			# Opening a directory for append fails without touching any user file.
			mkdir "$case_dir/report/disk-samples"
		fi
	fi
	mkfifo "$case_dir/sampling" "$case_dir/finish"
	exec 8<>"$case_dir/sampling"
	exec 9<>"$case_dir/finish"
	status=0
	env -i PATH="$scratch/bin:$tool_path" HOME="$case_dir/home" TMPDIR="$case_dir/tools-tmp" \
		CASE_DIR="$case_dir" REAL_GIT="$git_bin" REAL_REALPATH="$realpath_bin" \
		REAL_AWK="$awk_bin" REAL_GREP="$grep_bin" REAL_WC="$wc_bin" \
		GOCACHE="$case_dir/cache" GOMODCACHE="$case_dir/modules" GOTMPDIR="$case_dir/tmp" \
		GITHUB_REPOSITORY=devantler-tech/ksail GITHUB_REF=refs/heads/main \
		GITHUB_EVENT_NAME=workflow_dispatch GITHUB_RUN_ATTEMPT=1 RUNNER_OS=Linux GOMEMLIMIT=6GiB \
		ImageOS=synthetic ImageVersion=fixture GITHUB_SHA=1111111111111111111111111111111111111111 \
		GITHUB_RUN_ID=1 \
		"$timeout_bin" --signal=TERM --kill-after=1s 10s "$bash_bin" \
		"$here/collect-release-build-experiment.sh" "$case_dir/helper" "$case_dir/source" "$case_dir/report" 1 baseline \
		>"$case_dir/collector.log" 2>&1 || status=$?
	exec 8>&-
	exec 9>&-
	case_failed=0
	if [[ ! -e $case_dir/build-started || -e $case_dir/unexpected || -e $case_dir/forbidden-helper-stage ]]; then case_failed=1; fi
	if [[ -e $case_dir/report/phase.json || -e $case_dir/report/outputs.json ]]; then case_failed=1; fi
	if ! jq -e '.complete == false' "$case_dir/report/report.json" >/dev/null; then case_failed=1; fi
	if [[ $mode == combined ]]; then
		if [[ $status != 1 ]] || ! grep -Fxq 'release experiment collection: build_status=7 sampler_collection_status=1' "$case_dir/collector.log"; then case_failed=1; fi
	else
		if [[ $status != 7 ]] ||
			! grep -Fxq 'release experiment collection: build_status=7 sampler_collection_status=0' "$case_dir/collector.log" ||
			! grep -Fxq 'release experiment stage build failed: status=7' "$case_dir/collector.log"; then case_failed=1; fi
	fi
	if [[ $case_failed == 1 ]]; then
		printf 'FAIL: collector %s control, exit=%s\n' "$mode" "$status" >&2
		cat "$case_dir/collector.log" >&2
		if [[ -f $case_dir/unexpected ]]; then cat "$case_dir/unexpected" >&2; fi
		if [[ -f $case_dir/forbidden-helper-stage ]]; then cat "$case_dir/forbidden-helper-stage" >&2; fi
		failures=$((failures + 1))
	fi
done
[[ $failures == 0 ]] || exit 1
printf 'PASS: actual collector retains combined failures and the original build status (synthetic fixtures only)\n'
