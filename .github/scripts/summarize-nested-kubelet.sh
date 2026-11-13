#!/usr/bin/env bash
# Summarize a private nested kubelet journal (stdin) as fixed signal labels.
#
# Journal text is never printed. Every output line is
# "Nested kubelet journal signal: <label>", and <label> comes only from the
# allow-list below, plus "unclassified" (journal text, no known signal) and
# "unavailable" (no journal text at all).
#
# A signal matches only when every one of its phrases is on the same journal
# line, so an unrelated phrase elsewhere in the read cannot complete it. Each
# line yields at most its first matching signal, so the more specific signals
# come first. journalctl prints oldest first, so a long read keeps its newest
# bytes, and drops the first line when it was cut part-way.
set -euo pipefail
# Journal bytes need not be valid text in the runner's locale.
export LC_ALL=C

max_bytes=65536
buffer="$(mktemp)"
trap 'rm -f "$buffer"' EXIT

tail -c "$((max_bytes + 1))" >"$buffer"
size="$(wc -c <"$buffer" | tr -d '[:space:]')"
truncated=0
# The read was cut part-way through its first kept line unless the byte just
# before the kept bytes ends a line.
if [ "$size" -gt "$max_bytes" ] && [ "$(head -c 1 "$buffer" | od -An -c | tr -d ' ')" != '\n' ]; then
	truncated=1
fi

tail -c "$max_bytes" "$buffer" | tr '[:upper:]' '[:lower:]' | awk -v truncated="$truncated" '
BEGIN {
	# label <TAB> phrase [<TAB> phrase ...]; every phrase must be on one line.
	signals = \
		"inotify-instance-limit\tfsnotify watcher\ttoo many open files\n" \
		"inotify-watch-limit\tinotify_add_watch\tno space left on device\n" \
		"file-descriptors\ttoo many open files\n" \
		"storage-or-watch-limit\tno space left on device\n" \
		"swap-enabled\trunning with swap on is not supported\n" \
		"container-runtime\tcontainer runtime is down\n" \
		"runtime-connection\tvalidate service connection\n" \
		"cgroup-driver\truntime returned an unknown cgroup driver\n" \
		"memory-cgroup\tfailed to find memory cgroup\n" \
		"cgroup-mount\tcgroup mountpoint\n" \
		"read-only-filesystem\tread-only file system\n" \
		"kubelet-configuration\tfailed to load kubelet config\n" \
		"kubelet-flags\tfailed to validate kubelet flags\n" \
		"qos-cgroups\tfailed to initialize top level qos\n" \
		"cpu-manager\tfailed to initialize cpu manager\n" \
		"memory-manager\tfailed to initialize memory manager\n" \
		"kubelet-cgroup\tfailed to get the kubelet\047s cgroup\n" \
		"kubelet-privileges\tkubelet needs to run as uid 0\n" \
		"kubelet-startup\tfailed to run kubelet"
	count = split(signals, rows, "\n")
	for (i = 1; i <= count; i++) {
		fields = split(rows[i], parts, "\t")
		label[i] = parts[1]
		phrases[i] = fields - 1
		for (j = 2; j <= fields; j++) {
			phrase[i, j - 1] = parts[j]
		}
	}
	text = 0
}
NR == 1 && truncated == 1 { next }
{
	if ($0 ~ /[^ \t\r\v\f]/) {
		text = 1
	}
	for (i = 1; i <= count; i++) {
		matched = 1
		for (j = 1; j <= phrases[i]; j++) {
			if (index($0, phrase[i, j]) == 0) {
				matched = 0
				break
			}
		}
		if (matched) {
			seen[i] = 1
			break
		}
	}
}
END {
	found = 0
	for (i = 1; i <= count; i++) {
		if (seen[i]) {
			print "Nested kubelet journal signal: " label[i]
			found = 1
		}
	}
	if (!found) {
		print "Nested kubelet journal signal: " (text ? "unclassified" : "unavailable")
	}
}
'
