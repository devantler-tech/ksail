#!/usr/bin/env bash
# A short-lived registry error or a sustained quota refusal on the helper image
# must not fail a mirror restore, and a helper that cannot be obtained from any
# source must be named as the cause.
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
script="$action_dir/pull-helper-image.sh"
restore_action="$action_dir/../restore-mirror-cache/action.yaml"
warm_action="$action_dir/action.yaml"
helper_image=mirror.gcr.io/library/busybox:1.37
recovery_image=public.ecr.aws/docker/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec

fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"

# The stub fails the first $DOCKER_PRIMARY_FAILURES pulls of the helper image and
# the first $DOCKER_RECOVERY_FAILURES pulls of anything else, then succeeds. Any
# command other than `image inspect`, `pull` and `tag` is a failure of the test
# itself.
cat >"$fixture/bin/docker" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$DOCKER_CALLS"
case "$1" in
image) [ "$DOCKER_IMAGE_PRESENT" = true ] ;;
pull)
	pulls=$(grep -cFx "pull --quiet $3" "$DOCKER_CALLS")
	limit=$DOCKER_RECOVERY_FAILURES
	[ "$3" = "$HELPER_IMAGE" ] && limit=$DOCKER_PRIMARY_FAILURES
	if [ "$pulls" -le "$limit" ]; then
		echo 'toomanyrequests: quota exceeded' >&2
		exit 1
	fi
	;;
tag) [ "$DOCKER_TAG_WORKS" = true ] ;;
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

# run_case <image already present> <helper pulls that fail> <recovery pulls that fail> [<tag works>];
# sets status, output, primary_pulls, recovery_pulls, tags, sleeps.
run_case() {
	: >"$fixture/docker-calls"
	: >"$fixture/sleep-calls"
	status=0
	output=$(PATH="$fixture/bin:$PATH" DOCKER_CALLS="$fixture/docker-calls" \
		SLEEP_CALLS="$fixture/sleep-calls" DOCKER_IMAGE_PRESENT="$1" HELPER_IMAGE="$helper_image" \
		DOCKER_PRIMARY_FAILURES="$2" DOCKER_RECOVERY_FAILURES="$3" DOCKER_TAG_WORKS="${4:-true}" \
		bash "$script" 2>&1) || status=$?
	primary_pulls=$(grep -cFx "pull --quiet $helper_image" "$fixture/docker-calls" || true)
	recovery_pulls=$(grep -cFx "pull --quiet $recovery_image" "$fixture/docker-calls" || true)
	tags=$(grep -c '^tag ' "$fixture/docker-calls" || true)
	sleeps=$(tr '\n' ' ' <"$fixture/sleep-calls")
}

# A short-lived error recovers on the helper image, without touching the recovery copy.
run_case false 2 99
[ "$status" -eq 0 ] || fail "a pull that succeeds on the third attempt must pass: $output"
[ "$primary_pulls" -eq 3 ] || fail "expected three helper pulls, saw $primary_pulls"
[ "$recovery_pulls" -eq 0 ] || fail "a recovered helper pull must not use the recovery copy, saw $recovery_pulls"
[ "$tags" -eq 0 ] || fail "a pulled helper must not be retagged, saw $tags"
[ "$sleeps" = '5 10 ' ] || fail "expected a 5s then 10s wait, saw '$sleeps'"

# A sustained refusal recovers from the pinned digest, under the same image name.
run_case false 99 0
[ "$status" -eq 0 ] || fail "a verified recovery after a sustained refusal must pass: $output"
[ "$primary_pulls" -eq 3 ] || fail "the helper image must get all three attempts first, saw $primary_pulls"
[ "$recovery_pulls" -eq 1 ] || fail "expected one recovery pull, saw $recovery_pulls"
[ "$sleeps" = '5 10 ' ] || fail "no wait may separate the two sources, saw '$sleeps'"
[ "$(tail -n 1 "$fixture/docker-calls")" = "tag $recovery_image $helper_image" ] ||
	fail 'the recovered digest must be tagged with the helper image name, last'
case "$recovery_image" in
*@sha256:*) ;;
*) fail 'the recovery copy must be pulled by digest' ;;
esac

# The recovery copy has the same bounded retry.
run_case false 99 2
[ "$status" -eq 0 ] || fail "a recovery that succeeds on its third attempt must pass: $output"
[ "$recovery_pulls" -eq 3 ] || fail "expected three recovery pulls, saw $recovery_pulls"
[ "$sleeps" = '5 10 5 10 ' ] || fail "expected the same waits for each source, saw '$sleeps'"

# Total unavailability is named as the helper's failure, and nothing is tagged.
run_case false 99 99
[ "$status" -eq 1 ] || fail "a helper that never pulls must fail with status 1, saw $status"
[ "$primary_pulls" -eq 3 ] || fail "helper retries must stop after three pulls, saw $primary_pulls"
[ "$recovery_pulls" -eq 3 ] || fail "recovery retries must stop after three pulls, saw $recovery_pulls"
[ "$tags" -eq 0 ] || fail "a failed recovery must not tag anything, saw $tags"
[ "$sleeps" = '5 10 5 10 ' ] || fail "no wait may follow the last attempt, saw '$sleeps'"
grep -Fq "Failed to pull the mirror helper image from $helper_image and $recovery_image after 3 attempts each" <<<"$output" ||
	fail "the exhausted pull must name both sources: $output"
if grep -Eq 'Failed to (import|export)' <<<"$output"; then
	fail "an exhausted helper pull must not be reported as a mirror failure: $output"
fi

# A recovery that cannot take the helper's name must not pass: containers use that name.
run_case false 99 0 false
[ "$status" -eq 1 ] || fail "a failed tag must fail with status 1, saw $status"
grep -Fq "could not tag it as $helper_image" <<<"$output" || fail "a failed tag must be named: $output"


run_case true 99 99
[ "$status" -eq 0 ] || fail "an image that is already present must pass: $output"
[ "$primary_pulls" -eq 0 ] || fail "an image that is already present must not be pulled, saw $primary_pulls"
[ "$recovery_pulls" -eq 0 ] || fail "an image that is already present must not use the recovery copy, saw $recovery_pulls"

# Docker Hub stays out: every pull in every recovery path names one of the two sources.
run_case false 99 99
if grep -E '^pull ' "$fixture/docker-calls" | grep -Fvx -e "pull --quiet $helper_image" -e "pull --quiet $recovery_image" | grep -q .; then
	fail 'a pull named a source other than the helper image and its pinned copy'
fi

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
