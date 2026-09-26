#!/usr/bin/env bash
set -euo pipefail

subject="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-with-recovery.sh"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

# new_stub writes a ksail stub that keeps the test release's Helm state in a file, as Helm keeps it
# in a release secret. INSTALL_OUTCOMES lists, per install call, "ok", "timeout" (the release is
# recorded, then readiness fails) or "error" (nothing is recorded). An install while the release is
# recorded fails the way Helm does. DELETE_FAILS=1 makes every delete fail.
new_stub() {
	local dir=$1
	mkdir -p "$dir/bin"
	cat >"$dir/bin/ksail" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$STUB_DIR/calls"
case "$1 $2" in
"workload install")
	count=$(($(cat "$STUB_DIR/installs" 2>/dev/null || echo 0) + 1))
	echo "$count" >"$STUB_DIR/installs"
	if [ -e "$STUB_DIR/release" ]; then
		echo "Error: cannot reuse a name that is still in use" >&2
		exit 1
	fi
	outcome=$(cut -d' ' -f"$count" <<<"$INSTALL_OUTCOMES")
	case "$outcome" in
	ok) touch "$STUB_DIR/release" ;;
	timeout)
		touch "$STUB_DIR/release"
		echo "Error: context deadline exceeded" >&2
		exit 1
		;;
	*)
		echo "Error: install failed" >&2
		exit 1
		;;
	esac
	;;
"workload delete")
	if [ "${DELETE_FAILS:-0}" = 1 ]; then
		echo "Error: delete failed" >&2
		exit 1
	fi
	if [ "$3" = secret ] && [ "$4 $5" = "-l owner=helm,name=ksail-install-test" ]; then
		rm -f "$STUB_DIR/release"
	fi
	;;
*)
	echo "unexpected ksail invocation: $*" >&2
	exit 2
	;;
esac
STUB
	chmod +x "$dir/bin/ksail"
}

# run_case runs the subject with the stub and records its exit status.
run_case() {
	local dir=$1 outcomes=$2 delete_fails=${3:-0}
	new_stub "$dir"
	STUB_DIR="$dir" INSTALL_OUTCOMES="$outcomes" DELETE_FAILS="$delete_fails" \
		PATH="$dir/bin:$PATH" TIMEOUT=5m RETRY_DELAY=0 \
		"$subject" >"$dir/out" 2>&1 || echo $? >"$dir/status"
	[ -e "$dir/status" ] || echo 0 >"$dir/status"
}

installs() { cat "$1/installs" 2>/dev/null || echo 0; }
deletes() { grep -c '^workload delete' "$1/calls" || true; }

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# The reported failure: the first install records the release and times out, so the name is taken.
run_case "$scratch/timeout-then-ok" "timeout ok ok"
[ "$(cat "$scratch/timeout-then-ok/status")" = 0 ] ||
	fail "an install that timed out after recording the release did not recover: $(cat "$scratch/timeout-then-ok/out")"
[ "$(installs "$scratch/timeout-then-ok")" = 2 ] || fail "expected a second install after recovery"
grep -Fqx 'workload install ksail-install-test oci://ghcr.io/stefanprodan/charts/podinfo --version=6.7.1 --wait --timeout=5m' \
	"$scratch/timeout-then-ok/calls" || fail "the retry did not install the chart through ksail workload install"

# Recovery removes only the test-owned release.
grep '^workload delete' "$scratch/timeout-then-ok/calls" | sort >"$scratch/deletes"
printf '%s\n' \
	'workload delete deployment/ksail-install-test-podinfo --ignore-not-found' \
	'workload delete secret -l owner=helm,name=ksail-install-test --ignore-not-found' \
	'workload delete service/ksail-install-test-podinfo --ignore-not-found' >"$scratch/expected-deletes"
cmp -s "$scratch/deletes" "$scratch/expected-deletes" ||
	fail "recovery deleted something other than the test release: $(tr '\n' ';' <"$scratch/deletes")"

# A first-attempt success runs no recovery.
run_case "$scratch/ok" "ok"
[ "$(cat "$scratch/ok/status")" = 0 ] || fail "a successful install failed"
[ "$(deletes "$scratch/ok")" = 0 ] || fail "a successful install removed resources"

# A persistent failure still fails the test after three attempts.
run_case "$scratch/persistent" "timeout timeout timeout"
[ "$(cat "$scratch/persistent/status")" != 0 ] || fail "a persistent install failure passed"
[ "$(installs "$scratch/persistent")" = 3 ] || fail "expected three install attempts"
grep -Fq 'failed after 3 attempts' "$scratch/persistent/out" || fail "the final failure is not reported"

# A failed recovery stops the retries instead of being ignored.
run_case "$scratch/delete-fails" "timeout ok ok" 1
[ "$(cat "$scratch/delete-fails/status")" != 0 ] || fail "a failed recovery was ignored"
[ "$(installs "$scratch/delete-fails")" = 1 ] || fail "an install was retried after recovery failed"
grep -Fq 'could not remove the partial ksail-install-test release' "$scratch/delete-fails/out" ||
	fail "the recovery failure is not reported"

echo "PASS: install-with-recovery"
