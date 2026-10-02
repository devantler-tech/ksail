#!/usr/bin/env bash
# Check, seal and verify the five mirror archives passed between CI jobs.
set -euo pipefail

mode=${1:?check, prepare or verify is required}
directory=${2:?archive directory is required}
key=${3:?validated cache key is required}
archives=(docker.io.tar ghcr.io.tar quay.io.tar registry.k8s.io.tar ecr-public.aws.com.tar)

if [[ "$mode" != check && "$mode" != prepare && "$mode" != verify ]]; then
	echo "Unknown mirror artifact operation" >&2
	exit 1
fi

for archive in "${archives[@]}"; do
	path="$directory/$archive"
	if [[ ! -f "$path" || -L "$path" || ! -s "$path" ]] || ! tar -tf "$path" >/dev/null; then
		echo "Missing or invalid mirror archive: $archive" >&2
		exit 1
	fi
done

if [[ "$mode" == check ]]; then
	echo "Validated all five cached mirror archives"
	exit 0
fi

if [[ "$mode" == prepare ]]; then
	printf '%s\n' "$key" >"$directory/cache-key"
	(cd "$directory" && sha256sum -- "${archives[@]}" cache-key) >"$directory/SHA256SUMS"
else
	for metadata in cache-key SHA256SUMS; do
		if [[ ! -f "$directory/$metadata" || -L "$directory/$metadata" || ! -s "$directory/$metadata" ]]; then
			echo "Missing mirror artifact metadata: $metadata" >&2
			exit 1
		fi
	done
	if [[ "$(cat "$directory/cache-key")" != "$key" ]]; then
		echo "Mirror artifact does not match the validated producer key" >&2
		exit 1
	fi
	actual=$(cd "$directory" && sha256sum -- "${archives[@]}" cache-key)
	if [[ "$(cat "$directory/SHA256SUMS")" != "$actual" ]]; then
		echo "Mirror artifact checksum mismatch" >&2
		exit 1
	fi
fi

echo "Validated all five mirror archives ($mode)"
