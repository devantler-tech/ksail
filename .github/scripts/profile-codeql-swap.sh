#!/usr/bin/env bash
# Independently verify the operational helper's actual effect. Its intentional
# fail-open status is not evidence that the requested treatment was enabled.
set -euo pipefail
export LC_ALL=C
[[ $# == 2 && ("$1" == 0 || "$1" == 8) ]]
[[ "$(uname -s)" == Linux && "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]]
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
proc_root="${KSAIL_CODEQL_PROC_ROOT:-/proc}"
requested="$1" output="$2"

swap_total_kib() {
	awk '$1 == "SwapTotal:" {
    if (++count != 1 || NF != 3 || $2 !~ /^[0-9]+$/ || $3 != "kB") exit 2
    value=$2
  } END { if (count != 1) exit 2; print value }' "$proc_root/meminfo"
}

active_owned_swap() {
	awk 'NR == 1 { if ($1 != "Filename" || $2 != "Type" || $3 != "Size" || $4 != "Used" || $5 != "Priority") exit 2; next }
  { if (NF != 5 || ($2 != "file" && $2 != "partition") || $3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ || $5 !~ /^-?[0-9]+$/ || $4 > $3) exit 2 }
  $1 == "/mnt/ksail-codeql.swap" || $1 == "/ksail-codeql.swap" {
    if (++count != 1 || NF != 5 || $2 != "file" || $3 !~ /^[0-9]+$/) exit 2
    print $1, $3
  } END { if (NR == 0) exit 2 }' "$proc_root/swaps"
}

before="$(swap_total_kib)"
before_entry="$(active_owned_swap)"
[[ -z "$before_entry" ]]
mkdir -p "$(dirname "$output")"
KSAIL_CODEQL_SWAP_GIB="$requested" bash "$script_dir/codeql-autobuild-prepare.sh" >"$output.prepare.log" 2>&1
after="$(swap_total_kib)"
entry="$(active_owned_swap)"
activated=0
allocated=0
if [[ "$requested" == 0 ]]; then
	[[ "$before" == "$after" && -z "$entry" ]]
else
	[[ -n "$entry" ]]
	read -r swap_path size_kib <<<"$entry"
	activated=$((size_kib * 1024))
	# mkswap reserves a small header; both the active area and total delta must
	# agree, while the allocated private file must be exactly the requested size.
	((activated >= 8 * 1024 * 1024 * 1024 - 1024 * 1024 && activated <= 8 * 1024 * 1024 * 1024))
	((after - before == size_kib))
	file_info="$(stat -c '%s %a' "$swap_path")"
	read -r allocated mode <<<"$file_info"
	[[ "$allocated" == 8589934592 && "$mode" == 600 ]]
fi
jq -n --argjson requested "$requested" --argjson before "$((before * 1024))" \
	--argjson after "$((after * 1024))" --argjson activated "$activated" --argjson allocated "$allocated" \
	'{requestedAddedSwapGiB:$requested,originalSwapBytes:$before,preparedSwapBytes:$after,
    activatedSwapBytes:$activated,allocatedFileBytes:$allocated,allocationVerified:true}' >"$output.tmp"
mv "$output.tmp" "$output"
printf 'Verified %s GiB requested additional swap; observed active bytes=%s\n' "$requested" "$activated"
