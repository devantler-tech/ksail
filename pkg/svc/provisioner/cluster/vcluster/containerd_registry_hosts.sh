#!/bin/sh
set -eu
config_dir=${1:?containerd configuration directory is required}
snapshot=$(mktemp "$config_dir/ksail-config-XXXXXX")
candidate=$(mktemp "$config_dir/ksail-mirrors-XXXXXX")
trap 'rm -f "$snapshot" "$candidate"' EXIT
containerd config dump >"$snapshot"
has_registry_hosts_path() {
	awk '
    /^[[:space:]]*\[plugins[.]/ {
      registry = ($0 ~ /cri[.]v1[.]images.*[.]registry\]$/ ||
                  $0 ~ /grpc[.]v1[.]cri.*[.]registry\]$/)
      next
    }
    /^\[/ { registry = 0 }
    registry && /config_path[[:space:]]*=/ {
      path = $0
      sub(/^[^=]*=/, "", path)
      gsub(/["\047[:space:]]/, "", path)
      n = split(path, parts, ":")
      for (i = 1; i <= n; i++) found = found || parts[i] == "/etc/containerd/certs.d"
    }
    END { exit !found }
  ' "$1"
}
awk '
  /^[[:space:]]*\[/ {
    registry = ($0 ~ /io[.]containerd[.]cri[.]v1[.]images.*[.]registry\]$/ ||
                $0 ~ /io[.]containerd[.]grpc[.]v1[.]cri.*[.]registry\]$/)
  }
  registry && /^[[:space:]]*config_path[[:space:]]*=/ {
    found = 1
    value = $0
    sub(/^[^=]*=[[:space:]]*/, "", value)
    sub(/[[:space:]]*$/, "", value)
    if (value ~ /^["\047].*["\047]$/) value = substr(value, 2, length(value) - 2)
    count = split(value, paths, ":")
    has_hosts = 0
    for (i = 1; i <= count; i++) {
      if (paths[i] == "/etc/containerd/certs.d") has_hosts = 1
    }
    if (!has_hosts) {
      if (value != "") value = value ":"
      sub(/=.*/, "= \"" value "/etc/containerd/certs.d\"")
    }
  }
  { print }
  END { if (!found) exit 1 }
' "$snapshot" >"$candidate"
if cmp -s "$snapshot" "$candidate"; then
	exit 0
fi
chmod 0600 "$candidate"
mv "$candidate" "$config_dir/config.toml"
systemctl restart containerd
systemctl is-active --quiet containerd
containerd config dump >"$snapshot"
if ! has_registry_hosts_path "$snapshot"; then
	echo 'containerd effective registry configuration lacks the mirror hosts path' >&2
	exit 1
fi
