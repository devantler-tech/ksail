#!/usr/bin/env bash
# Consume private journal text, publishing only fixed observed categories.
set -euo pipefail
text="$(head -c 65536)"
text="${text,,}"
seen=false
while IFS='|' read -r needle label; do
  if [[ "$text" == *"$needle"* ]]; then
    printf 'Nested kubelet journal signal: %s\n' "$label"
    seen=true
  fi
done <<'SIGNALS'
too many open files|file-descriptors
no space left on device|storage-full
running with swap on is not supported|swap-enabled
container runtime is down|container-runtime
validate service connection|runtime-connection
cgroup driver|cgroup-driver
failed to find memory cgroup|memory-cgroup
cgroup mountpoint|cgroup-mount
read-only file system|read-only-filesystem
failed to load kubelet config|kubelet-configuration
failed to validate kubelet flags|kubelet-flags
failed to initialize top level qos|qos-cgroups
failed to initialize cpu manager|cpu-manager
failed to initialize memory manager|memory-manager
failed to create fsnotify watcher|filesystem-watcher
failed to get the kubelet's cgroup|kubelet-cgroup
kubelet needs to run as uid 0|kubelet-privileges
failed to run kubelet|kubelet-startup
SIGNALS
if [[ "$seen" == false ]]; then
  echo 'Nested kubelet journal signal: unclassified'
fi
