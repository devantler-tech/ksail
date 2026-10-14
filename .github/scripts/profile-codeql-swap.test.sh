#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin"
printf '#!/usr/bin/env bash\necho Linux\n' >"$scratch/bin/uname"
printf '#!/usr/bin/env bash\nprintf "Avail\\n60G\\n"\n' >"$scratch/bin/df"
printf '#!/usr/bin/env bash\necho "1024 bash"\n' >"$scratch/bin/ps"
cat >"$scratch/bin/stat" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == '-c %s %a /mnt/ksail-codeql.swap' ]]
printf '8589934592 %s\n' "${STUB_MODE:-600}"
[[ "${STUB_STAT_FAIL:-false}" != true ]] || exit 23
EOF
cat >"$scratch/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$STUB_STATE/sudo.log"
if [[ "$2" == swapon ]]; then
  [[ "${STUB_FAIL:-false}" != true ]] || exit 1
  printf 'MemTotal: 16384000 kB\nMemAvailable: 4000000 kB\nSwapTotal: 11534332 kB\nSwapFree: 11534332 kB\n' >"$KSAIL_CODEQL_PROC_ROOT/meminfo"
  printf '/mnt/ksail-codeql.swap file 8388604 0 -2\n' >>"$KSAIL_CODEQL_PROC_ROOT/swaps"
fi
EOF
chmod +x "$scratch/bin/"*

run_case() {
	local name="$1" arm="$2" expected="$3" status=0
	shift 3
	local state="$scratch/$name"
	mkdir -p "$state/proc"
	printf 'MemTotal: 16384000 kB\nMemAvailable: 4000000 kB\nSwapTotal: 3145728 kB\nSwapFree: 3145728 kB\n' >"$state/proc/meminfo"
	printf 'Filename Type Size Used Priority\n/swapfile file 3145728 0 -2\n' >"$state/proc/swaps"
	if [[ "$name" == duplicate-field ]]; then printf 'SwapTotal: 3145728 kB\n' >>"$state/proc/meminfo"; fi
	if [[ "$name" == invalid-inventory ]]; then printf 'partial inventory\n' >"$state/proc/swaps"; fi
	env PATH="$scratch/bin:$PATH" STUB_STATE="$state" GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted \
		RUNNER_TEMP="$state" KSAIL_CODEQL_PROC_ROOT="$state/proc" "$@" \
		bash "$script_dir/profile-codeql-swap.sh" "$arm" "$state/swap.json" >"$state/log" 2>&1 || status=$?
	if [[ "$expected" == pass ]]; then
		[[ "$status" == 0 ]] || {
			cat "$state/log"
			exit 1
		}
		jq -e --argjson arm "$arm" '.requestedAddedSwapGiB == $arm and .allocationVerified and .originalSwapBytes == 3221225472' "$state/swap.json" >/dev/null
		if [[ "$arm" == 0 ]]; then
			[[ ! -s "$state/sudo.log" ]]
			jq -e '.preparedSwapBytes == .originalSwapBytes and .activatedSwapBytes == 0' "$state/swap.json" >/dev/null
		else
			jq -e '.activatedSwapBytes == 8589930496 and .allocatedFileBytes == 8589934592 and
        (.preparedSwapBytes - .originalSwapBytes) == .activatedSwapBytes' "$state/swap.json" >/dev/null
		fi
	else
		[[ "$status" != 0 && ! -e "$state/swap.json" ]] || {
			printf 'FAIL: %s accepted invalid preparation\n' "$name"
			exit 1
		}
	fi
	printf 'PASS: %s\n' "$name"
}
run_case baseline 0 pass
run_case treatment 8 pass
run_case allocation-failed 8 fail STUB_FAIL=true
run_case exposed-file 8 fail STUB_MODE=644
run_case partial-failed-stat 8 fail STUB_STAT_FAIL=true
run_case duplicate-field 0 fail
run_case invalid-inventory 0 fail
printf 'PASS: verified actual swap preparation, allocation failure and unknown reads\n'
