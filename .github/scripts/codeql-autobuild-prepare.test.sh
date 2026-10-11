#!/usr/bin/env bash
# Tests for codeql-autobuild-prepare.sh and the Makefile route that reaches it. Every system
# command the script would run on a runner (sudo, df, ps, uname) is replaced by a recorder.
set -euo pipefail
export LC_ALL=C

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
script="$script_dir/codeql-autobuild-prepare.sh"
work=$(mktemp -d)
watched=""
checks=0

cleanup() {
	[[ -z "$watched" ]] || kill "$watched" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	exit 1
}

# expect_contains fails unless the text holds the wanted fragment.
expect_contains() {
	[[ "$1" == *"$2"* ]] || fail "$3: expected to find '$2' in: $1"
	checks=$((checks + 1))
}

# expect_equal fails unless both values are identical.
expect_equal() {
	[[ "$1" == "$2" ]] || fail "$3: expected '$2', got '$1'"
	checks=$((checks + 1))
}

mkdir -p "$work/bin"
cat >"$work/bin/uname" <<'STUB'
#!/usr/bin/env bash
echo Linux
STUB
cat >"$work/bin/sudo" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_DIR/sudo.log"
[[ "$*" != *"${STUB_SUDO_FAIL:-no-such-command}"* ]]
STUB
cat >"$work/bin/df" <<'STUB'
#!/usr/bin/env bash
directory="${*: -1}"
printf 'Avail\n'
if [[ "$directory" == /mnt ]]; then printf '%sG\n' "$STUB_MNT_GIB"; else printf '%sG\n' "$STUB_ROOT_GIB"; fi
STUB
cat >"$work/bin/ps" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == *ppid=* ]]; then printf '  %s\n' "$STUB_WATCHED"; else printf '13000000 go-extractor\n  2048 bash\n'; fi
STUB
chmod +x "$work/bin/uname" "$work/bin/sudo" "$work/bin/df" "$work/bin/ps"

sleep 60 &
watched=$!
mkdir -p "$work/proc/$watched/fd"
printf 'MemTotal:       16384000 kB\nMemAvailable:    1024000 kB\nSwapTotal:       4096000 kB\nSwapFree:        2048000 kB\n' \
	>"$work/proc/meminfo"

# prepare runs the script as a hosted runner would, with a fresh job directory per case.
prepare() {
	local case_name="$1"
	shift
	mkdir -p "$work/$case_name"
	: >"$work/$case_name/sudo.log"
	: >"$work/proc/$watched/fd/2"
	env PATH="$work/bin:$PATH" STUB_DIR="$work/$case_name" STUB_WATCHED="$watched" \
		STUB_MNT_GIB=60 STUB_ROOT_GIB=20 GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted \
		RUNNER_TEMP="$work/$case_name" KSAIL_CODEQL_PROC_ROOT="$work/proc" \
		KSAIL_CODEQL_SAMPLE_SECONDS=1 "$@" bash "$script" 4242
}

# samples prints how many lines the script has appended to the watched process's stream.
samples() {
	grep -c 'ksail-codeql-memory: t=+' "$work/proc/$watched/fd/2" || true
}

# wait_for_sample waits a few seconds for the first appended line.
wait_for_sample() {
	local tries=0
	until [[ "$(samples)" -gt 0 ]]; do
		tries=$((tries + 1))
		[[ "$tries" -lt 50 ]] || fail "$1: no sample reached the watched process"
		sleep 0.2
	done
}

# Away from a hosted runner the script does nothing at all.
output=$(env PATH="$work/bin:$PATH" STUB_DIR="$work" GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=self-hosted \
	RUNNER_TEMP="$work/elsewhere" bash "$script" 4242)
expect_contains "$output" 'nothing to do' 'self-hosted runner'
[[ ! -e "$work/sudo.log" && ! -e "$work/elsewhere" ]] || fail 'self-hosted runner: the script acted'
output=$(env -u GITHUB_ACTIONS PATH="$work/bin:$PATH" STUB_DIR="$work" RUNNER_ENVIRONMENT=github-hosted \
	RUNNER_TEMP="$work/elsewhere" bash "$script" 4242)
expect_contains "$output" 'nothing to do' 'outside GitHub Actions'
[[ ! -e "$work/sudo.log" && ! -e "$work/elsewhere" ]] || fail 'outside GitHub Actions: the script acted'

# On a hosted runner it reports memory, adds swap on the spare disk and starts sampling.
output=$(prepare spare-disk)
expect_contains "$output" 'before mem_total=16000MiB mem_available=1000MiB swap_total=4000MiB swap_used=2000MiB largest=go-extractor=12695MiB' 'spare disk'
expect_contains "$output" 'added 8 GiB of swap at /mnt/ksail-codeql.swap (60 GiB were free there)' 'spare disk'
expect_contains "$output" 'ksail-codeql-memory: after mem_total=' 'spare disk'
expect_contains "$output" "sampling every 1s until process $watched exits" 'spare disk'
expect_equal "$(cat "$work/spare-disk/sudo.log")" "-n fallocate -l 8G /mnt/ksail-codeql.swap
-n chmod 600 /mnt/ksail-codeql.swap
-n mkswap /mnt/ksail-codeql.swap
-n swapon /mnt/ksail-codeql.swap" 'spare disk commands'
wait_for_sample 'spare disk'
expect_contains "$(cat "$work/proc/$watched/fd/2")" 't=+0s mem_total=16000MiB mem_available=1000MiB' 'first sample'

# A second call in the same job changes nothing.
output=$(prepare spare-disk)
expect_contains "$output" 'runner already prepared in this job' 'second call'
expect_equal "$(cat "$work/spare-disk/sudo.log")" '' 'second call commands'

# A job directory that cannot be created is reported as such, and nothing is done.
mkdir -p "$work/unwritable"
: >"$work/unwritable/sudo.log"
output=$(env PATH="$work/bin:$PATH" STUB_DIR="$work/unwritable" GITHUB_ACTIONS=true \
	RUNNER_ENVIRONMENT=github-hosted RUNNER_TEMP="$work/unwritable/missing" bash "$script" 4242)
expect_contains "$output" "cannot create $work/unwritable/missing/ksail-codeql-autobuild; nothing done" 'missing job directory'
expect_equal "$(cat "$work/unwritable/sudo.log")" '' 'missing job directory commands'

# Without room on the spare disk the root disk is used only when it has more to spare.
output=$(prepare root-disk STUB_MNT_GIB=12 STUB_ROOT_GIB=40)
expect_contains "$output" 'added 8 GiB of swap at /ksail-codeql.swap (40 GiB were free there)' 'root disk'
output=$(prepare no-disk STUB_MNT_GIB=12 STUB_ROOT_GIB=27)
expect_contains "$output" 'no disk has room for 8 GiB of swap; none added' 'no disk'
expect_contains "$output" 'sampling every 1s' 'no disk still samples'
expect_equal "$(cat "$work/no-disk/sudo.log")" '' 'no disk commands'

# A refused swap is reported and cleaned up, and the build still goes on.
output=$(prepare refused STUB_SUDO_FAIL=swapon)
expect_contains "$output" 'could not enable swap at /mnt/ksail-codeql.swap' 'refused swap'
expect_contains "$(tail -n 1 "$work/refused/sudo.log")" '-n rm -f /mnt/ksail-codeql.swap' 'refused swap cleanup'

# Without a process to write to it says so instead of sampling.
output=$(prepare no-caller STUB_WATCHED=1)
expect_contains "$output" 'no samples will follow' 'no caller'

# Sampling ends when the watched process does.
kill "$watched"
wait "$watched" 2>/dev/null || true
sleep 3
settled=$(samples)
sleep 2
expect_equal "$(samples)" "$settled" 'sampling after the watched process exited'
watched=""

# The Makefile routes only CodeQL's plain `make` to the script.
routed=$(env GITHUB_ACTIONS=true CODEQL_EXTRACTOR_GO_ROOT=/opt/codeql/go make -n -C "$repo_root")
expect_contains "$routed" 'bash .github/scripts/codeql-autobuild-prepare.sh' 'make under CodeQL'
plain=$(env -u GITHUB_ACTIONS -u CODEQL_EXTRACTOR_GO_ROOT make -n -C "$repo_root")
[[ "$plain" != *codeql-autobuild-prepare.sh* ]] || fail 'plain make must still list the targets'
expect_contains "$plain" 'grep -hE' 'plain make'
ci_only=$(env -u CODEQL_EXTRACTOR_GO_ROOT GITHUB_ACTIONS=true make -n -C "$repo_root")
[[ "$ci_only" != *codeql-autobuild-prepare.sh* ]] || fail 'make in other CI jobs must still list the targets'
checks=$((checks + 2))

printf 'PASS: %d checks\n' "$checks"
