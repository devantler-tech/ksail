#!/usr/bin/env bash
# A short-lived registry error on the helper image must not fail a mirror
# restore, and a helper that cannot be obtained must be named as the cause.
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
script="$action_dir/pull-helper-image.sh"
restore_action="$action_dir/../restore-mirror-cache/action.yaml"
warm_action="$action_dir/action.yaml"
helper_image=mirror.gcr.io/library/busybox:1.37

fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"

# The stub fails the first $DOCKER_PULL_FAILURES pulls, then succeeds. Any
# command other than `image inspect` and `pull` is a failure of the test itself.
cat >"$fixture/bin/docker" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$DOCKER_CALLS"
case "$1" in
image) [ "$DOCKER_IMAGE_PRESENT" = true ] ;;
pull)
	pulls=$(grep -c '^pull ' "$DOCKER_CALLS")
	if [ "$pulls" -le "$DOCKER_PULL_FAILURES" ]; then
		echo 'unknown blob' >&2
		exit 1
	fi
	;;
*) exit 99 ;;
esac
SH
cat >"$fixture/bin/sleep" <<'SH'
#!/bin/sh
printf '%s\n' "$1" >>"$SLEEP_CALLS"
SH
chmod +x "$fixture/bin/docker" "$fixture/bin/sleep"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# run_case <image already present> <pulls that fail>; sets status, output, pulls, sleeps.
run_case() {
	: >"$fixture/docker-calls"
	: >"$fixture/sleep-calls"
	status=0
	output=$(PATH="$fixture/bin:$PATH" DOCKER_CALLS="$fixture/docker-calls" \
		SLEEP_CALLS="$fixture/sleep-calls" DOCKER_IMAGE_PRESENT="$1" \
		DOCKER_PULL_FAILURES="$2" bash "$script" 2>&1) || status=$?
	pulls=$(grep -c '^pull ' "$fixture/docker-calls" || true)
	sleeps=$(tr '\n' ' ' <"$fixture/sleep-calls")
}

run_case false 2
[ "$status" -eq 0 ] || fail "a pull that succeeds on the third attempt must pass: $output"
[ "$pulls" -eq 3 ] || fail "expected three pulls, saw $pulls"
[ "$sleeps" = '5 10 ' ] || fail "expected a 5s then 10s wait, saw '$sleeps'"
grep -Fxq "pull --quiet $helper_image" "$fixture/docker-calls" || fail 'the pinned helper image must be the one pulled'

run_case false 99
[ "$status" -eq 1 ] || fail "a helper that never pulls must fail with status 1, saw $status"
[ "$pulls" -eq 3 ] || fail "retries must stop after three pulls, saw $pulls"
[ "$sleeps" = '5 10 ' ] || fail "no wait may follow the last attempt, saw '$sleeps'"
grep -Fq "Failed to pull the mirror helper image $helper_image after 3 attempts" <<<"$output" ||
	fail "the exhausted pull must name the helper image: $output"
if grep -Eq 'Failed to (import|export)' <<<"$output"; then
	fail "an exhausted helper pull must not be reported as a mirror failure: $output"
fi

run_case true 99
[ "$status" -eq 0 ] || fail "an image that is already present must pass: $output"
[ "$pulls" -eq 0 ] || fail "an image that is already present must not be pulled, saw $pulls"

# Both actions must obtain the helper before their first helper container, and
# every helper container must use the image the shared script pulls.
# Prints nothing when the text is absent, so the caller reports the failure.
first_line() { { grep -n -m1 -F -- "$1" "$2" || true; } | cut -d: -f1; }
for action in "$restore_action" "$warm_action"; do
	prepull=$(first_line 'pull-helper-image.sh' "$action")
	first_use=$(first_line "$helper_image \\" "$action")
	if [ -z "$prepull" ] || [ -z "$first_use" ] || [ "$prepull" -ge "$first_use" ]; then
		fail "$action must pull the helper image before its first helper container"
	fi
	if grep -n 'busybox' "$action" | grep -v 'docker.io/library/busybox:1.36.1' |
		grep 'busybox:' | grep -Fvq "$helper_image"; then
		fail "$action starts a helper container from an image the shared script does not pull"
	fi
done
# The restore must obtain the helper before it creates or fills any volume.
volume_create=$(first_line 'docker volume create' "$restore_action")
prepull=$(first_line 'pull-helper-image.sh' "$restore_action")
if [ -z "$prepull" ] || [ -z "$volume_create" ] || [ "$prepull" -ge "$volume_create" ]; then
	fail 'the restore must pull the helper image before creating a mirror volume'
fi
# The warm action must obtain it before it discards the rejected archives.
discard=$(first_line 'rm -rf -- /tmp/mirror-cache' "$warm_action")
prepull=$(first_line 'pull-helper-image.sh' "$warm_action")
if [ -z "$prepull" ] || [ -z "$discard" ] || [ "$prepull" -ge "$discard" ]; then
	fail 'the warm action must pull the helper image before replacing mirror archives'
fi

echo 'PASS: pull-helper-image'
