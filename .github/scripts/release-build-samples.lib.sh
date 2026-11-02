#!/usr/bin/env bash
# Shared only by the fixed collector and its offline regression tests.
sample_once() {
  local source_dir=$1 report_dir=$2 free
  free=$(df -Pk "$source_dir" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" | awk 'NR>1 {printf "%.0f\n",$4*1024}')
  [[ $(wc -l <<< "$free") == 4 ]]
  free=$(sort -n <<< "$free" | head -n 1)
  [[ $free =~ ^[0-9]+$ ]]
  printf '%s %s\n' "$(date +%s.%N)" "$free" >> "$report_dir/disk-samples"
}

validate_disk_samples() {
  awk -v start="$2" -v end="$3" '
    NR==1 {if ($1-start>10) exit 1; previous=$1}
    {if (NF!=2 || $2 !~ /^[0-9]+$/ || $1<previous || $1-previous>15) exit 1; previous=$1}
    END {if (NR<2 || end-previous>10) exit 1}' "$1"
}
