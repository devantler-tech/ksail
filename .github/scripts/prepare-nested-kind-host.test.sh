#!/usr/bin/env bash
# Exercise the actual CI host setup without changing a host sysctl.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin" "$tmp/state"
cat >"$tmp/bin/sysctl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
operation=$1
key=${2%%=*}
case "$key" in
fs.inotify.max_user_instances) file=instances ;;
fs.inotify.max_user_watches) file=watches ;;
*) exit 9 ;;
esac
if [[ "$operation" == -n ]]; then
	[[ "${FAIL_READ_KEY:-}" != "$key" ]] || exit 7
	if [[ "${INVALID_READBACK:-0}" == 1 && -s "$STATE_DIR/writes" ]]; then
		echo invalid
		exit 0
	fi
	cat "$STATE_DIR/$file"
elif [[ "$operation" == -w ]]; then
	printf '%s\n' "$2" >>"$STATE_DIR/writes"
	[[ "${FAIL_WRITE:-0}" == 0 ]] || exit 8
	if [[ "${IGNORE_WRITE:-0}" == 0 ]]; then
		printf '%s\n' "${2#*=}" >"$STATE_DIR/$file"
	fi
else
	exit 9
fi
SH
cat >"$tmp/bin/sudo" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -n && "$2" == sysctl ]] || exit 9
shift
exec "$@"
SH
chmod +x "$tmp/bin/sysctl" "$tmp/bin/sudo"
reset_state() {
	printf '%s\n' "$1" >"$tmp/state/instances"
	printf '%s\n' "$2" >"$tmp/state/watches"
	: >"$tmp/state/writes"
}
prepare() {
	env PATH="$tmp/bin:$PATH" STATE_DIR="$tmp/state" \
		FAIL_READ_KEY="${FAIL_READ_KEY:-}" FAIL_WRITE="${FAIL_WRITE:-0}" IGNORE_WRITE="${IGNORE_WRITE:-0}" \
		INVALID_READBACK="${INVALID_READBACK:-0}" \
		bash "$root/.github/scripts/prepare-nested-kind-host.sh"
}
reject() {
	if prepare >"$tmp/rejected.log" 2>&1; then
		echo 'unsafe host preparation was accepted' >&2
		exit 1
	fi
}

reset_state 128 8192
prepare
[[ "$(cat "$tmp/state/instances")" == 512 && "$(cat "$tmp/state/watches")" == 524288 ]]
[[ "$(wc -l <"$tmp/state/writes")" -eq 2 ]]
reset_state 8192 1048576
prepare
[[ "$(cat "$tmp/state/instances")" == 8192 && "$(cat "$tmp/state/watches")" == 1048576 ]]
[[ ! -s "$tmp/state/writes" ]]
reset_state 1024 8192
prepare
[[ "$(cat "$tmp/state/instances")" == 1024 && "$(cat "$tmp/state/watches")" == 524288 ]]
[[ "$(cat "$tmp/state/writes")" == fs.inotify.max_user_watches=524288 ]]

# Validate both observations before either write: a bad second value must not
# authorize even the otherwise valid first update.
for value in invalid 0 0128 99999999999999999999; do
	reset_state 128 "$value"
	reject
	[[ ! -s "$tmp/state/writes" ]]
done
reset_state 128 8192
FAIL_READ_KEY=fs.inotify.max_user_watches reject
[[ ! -s "$tmp/state/writes" ]]
reset_state 128 8192
FAIL_WRITE=1 reject
reset_state 128 8192
IGNORE_WRITE=1 reject
reset_state 128 8192
INVALID_READBACK=1 reject
echo 'nested Kind host: minimums, higher-value preservation, partial reads and write/readback failures verified'
