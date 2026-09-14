#!/usr/bin/env bash
# Behaviour test for style-clean-cask-branch.sh against real local git remotes and a fake `brew`.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
subject="${script_dir}/style-clean-cask-branch.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
branch="goreleaser/ksail"

# Isolate git from the host: no signing, URL rewrites, or hooks from global or system config.
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL="${tmp_dir}/gitconfig"
export GIT_TERMINAL_PROMPT=0
cat >"${GIT_CONFIG_GLOBAL}" <<'EOF'
[user]
	name = fixture
	email = fixture@example.invalid
[init]
	defaultBranch = main
EOF

fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"
# Offense model: `BAD` is autocorrectable and `STUCK` is not. When RACE_MODE is set, another writer
# pushes to the remote from its own clone during the first RACE_TIMES `--fix` calls: `fix` models the
# tap CI landing its autocorrection first, `rewrite` models a fresh, still-dirty GoReleaser write.
cat >"${fake_bin}/brew" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == style ]] || exit 2
if [[ "$2" == --fix ]]; then
	file="$3"
	if [[ -n "${RACE_MODE:-}" ]]; then
		calls=0
		if [[ -f "${RACE_STATE}" ]]; then
			calls="$(<"${RACE_STATE}")"
		fi
		calls=$((calls + 1))
		printf '%s\n' "${calls}" >"${RACE_STATE}"
		if [[ "${calls}" -le "${RACE_TIMES}" ]]; then
			git -C "${RACE_CLONE}" pull --quiet --ff-only
			case "${RACE_MODE}" in
			fix) sed -i.bak 's/BAD/GOOD/g' "${RACE_CLONE}/Casks/ksail.rb" ;;
			rewrite) printf 'rewrite %s BAD\n' "${calls}" >>"${RACE_CLONE}/Casks/ksail.rb" ;;
			esac
			rm -f "${RACE_CLONE}/Casks/ksail.rb.bak"
			git -C "${RACE_CLONE}" commit --quiet -am "racer ${RACE_MODE} ${calls}"
			git -C "${RACE_CLONE}" push --quiet
		fi
	fi
	sed -i.bak 's/BAD/GOOD/g' "${file}"
	rm -f "${file}.bak"
	if grep -q STUCK "${file}"; then
		exit 1
	fi
	exit 0
fi
if grep -Eq 'BAD|STUCK' "$2"; then
	exit 1
fi
exit 0
EOF
chmod +x "${fake_bin}/brew"
export PATH="${fake_bin}:${PATH}"

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	if [[ -n "${2:-}" && -f "$2" ]]; then
		sed 's/^/  | /' "$2" >&2
	fi
	exit 1
}

# new_remote <dir> <cask content>: a bare remote holding one generated cask commit on the branch,
# plus a second clone the racer writes from.
new_remote() {
	local dir="$1" content="$2"
	mkdir -p "${dir}/seed/Casks"
	git init --quiet --bare "${dir}/remote.git"
	git -C "${dir}/remote.git" symbolic-ref HEAD "refs/heads/${branch}"
	git init --quiet "${dir}/seed"
	printf '%s\n' "${content}" >"${dir}/seed/Casks/ksail.rb"
	git -C "${dir}/seed" add Casks/ksail.rb
	git -C "${dir}/seed" commit --quiet -m 'Brew cask update for ksail version v1.0.0'
	git -C "${dir}/seed" push --quiet "${dir}/remote.git" "HEAD:refs/heads/${branch}"
	git clone --quiet --branch "${branch}" "${dir}/remote.git" "${dir}/racer"
}

run_status=0
run_subject() {
	local dir="$1"
	shift
	set +e
	"${subject}" --remote "${dir}/remote.git" --branch "${branch}" --cask-name ksail "$@" >"${dir}/out" 2>&1
	run_status=$?
	set -e
}

tip_cask() {
	git -C "$1/remote.git" show "refs/heads/${branch}:Casks/ksail.rb"
}

tip_subject() {
	git -C "$1/remote.git" log -1 --format=%s "refs/heads/${branch}${2:-}"
}

commit_count() {
	git -C "$1/remote.git" rev-list --count "refs/heads/${branch}"
}

# An already-clean tip needs no commit.
s="${tmp_dir}/clean"
new_remote "${s}" 'cask GOOD'
run_subject "${s}"
[[ "${run_status}" -eq 0 ]] || fail 'an already-clean cask must be reported clean' "${s}/out"
[[ "$(commit_count "${s}")" -eq 1 ]] || fail 'an already-clean cask must not gain a commit' "${s}/out"

# A fixable cask gets the autocorrection pushed.
s="${tmp_dir}/fixable"
new_remote "${s}" 'cask BAD'
run_subject "${s}"
[[ "${run_status}" -eq 0 ]] || fail 'a fixable cask must be pushed clean' "${s}/out"
[[ "$(tip_subject "${s}")" == 'style: brew style --fix generated cask' ]] ||
	fail 'the autocorrection must land as the branch tip' "${s}/out"
if tip_cask "${s}" | grep -q BAD; then
	fail 'the pushed cask must not keep the offense' "${s}/out"
fi

# #6628: the tap CI autocorrects the branch first, so this push is rejected. The new tip is already
# clean, so the handoff must succeed without a second style commit.
s="${tmp_dir}/race-fixed"
new_remote "${s}" 'cask BAD'
export RACE_MODE=fix RACE_TIMES=1 RACE_STATE="${s}/race" RACE_CLONE="${s}/racer"
run_subject "${s}"
unset RACE_MODE RACE_TIMES RACE_STATE RACE_CLONE
[[ "${run_status}" -eq 0 ]] || fail 'a branch another writer already cleaned must be reported clean' "${s}/out"
grep -Fq 'rejected as non-fast-forward' "${s}/out" ||
	fail 'the rejected push must be named as a non-fast-forward race' "${s}/out"
[[ "$(tip_subject "${s}")" == 'racer fix 1' && "$(commit_count "${s}")" -eq 2 ]] ||
	fail 'the other writer'\''s autocorrection must remain the tip with no duplicate style commit' "${s}/out"

# The branch moves to a new, still-dirty write: the fix is re-applied on top of it.
s="${tmp_dir}/race-rewrite"
new_remote "${s}" 'cask BAD'
export RACE_MODE=rewrite RACE_TIMES=1 RACE_STATE="${s}/race" RACE_CLONE="${s}/racer"
run_subject "${s}"
unset RACE_MODE RACE_TIMES RACE_STATE RACE_CLONE
[[ "${run_status}" -eq 0 ]] || fail 'a re-dirtied branch must be autocorrected on its new tip' "${s}/out"
[[ "$(tip_subject "${s}")" == 'style: brew style --fix generated cask' ]] ||
	fail 'the retried autocorrection must land as the tip' "${s}/out"
[[ "$(tip_subject "${s}" '~1')" == 'racer rewrite 1' ]] ||
	fail 'the retried autocorrection must be built on the other writer'\''s commit, not replace it' "${s}/out"
if tip_cask "${s}" | grep -q BAD; then
	fail 'the retried cask must not keep any offense' "${s}/out"
fi

# A branch that keeps moving is bounded by --attempts and never reported clean.
s="${tmp_dir}/race-forever"
new_remote "${s}" 'cask BAD'
export RACE_MODE=rewrite RACE_TIMES=99 RACE_STATE="${s}/race" RACE_CLONE="${s}/racer"
run_subject "${s}" --attempts 2
unset RACE_MODE RACE_TIMES RACE_STATE RACE_CLONE
[[ "${run_status}" -eq 1 ]] || fail 'a branch that never settles must fail' "${s}/out"
[[ "$(<"${s}/race")" -eq 2 ]] || fail 'retries must stop at --attempts' "${s}/out"
grep -Fq 'kept moving' "${s}/out" || fail 'exhausted retries must be named' "${s}/out"

# The branch moves while this run finds nothing to fix: the new, dirty tip must be checked, not the
# clean one this run cloned.
s="${tmp_dir}/moved-before-verify"
new_remote "${s}" 'cask GOOD'
export RACE_MODE=rewrite RACE_TIMES=1 RACE_STATE="${s}/race" RACE_CLONE="${s}/racer"
run_subject "${s}"
unset RACE_MODE RACE_TIMES RACE_STATE RACE_CLONE
[[ "${run_status}" -eq 0 ]] || fail 'a branch that moved before verification must be re-checked and fixed' "${s}/out"
grep -Fq 'moved before verification' "${s}/out" || fail 'the move before verification must be named' "${s}/out"
[[ "$(tip_subject "${s}")" == 'style: brew style --fix generated cask' && "$(tip_subject "${s}" '~1')" == 'racer rewrite 1' ]] ||
	fail 'the moved tip must be autocorrected, not reported clean from the stale clone' "${s}/out"

# The branch moves right after this run's push lands: verification must judge that new tip.
s="${tmp_dir}/moved-after-push"
new_remote "${s}" 'cask BAD'
cat >"${s}/remote.git/hooks/post-receive" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -f moved-once ]]; then
	exit 0
fi
touch moved-once
racer="$(cd .. && pwd)/racer"
# Leave this receive's git environment before driving a second, nested push.
# shellcheck disable=SC2046
unset $(git rev-parse --local-env-vars)
git -C "${racer}" pull --quiet --ff-only
printf 'rewrite after push BAD\n' >>"${racer}/Casks/ksail.rb"
git -C "${racer}" commit --quiet -am 'racer rewrite after push'
git -C "${racer}" push --quiet
EOF
chmod +x "${s}/remote.git/hooks/post-receive"
run_subject "${s}"
[[ -f "${s}/remote.git/moved-once" ]] || fail 'fixture: the move after the push must have happened' "${s}/out"
[[ "${run_status}" -eq 0 ]] || fail 'a branch that moved after the push must be re-checked and fixed' "${s}/out"
grep -Fq 'moved before verification' "${s}/out" || fail 'the move after the push must be named' "${s}/out"
[[ "$(tip_subject "${s}")" == 'style: brew style --fix generated cask' && "$(tip_subject "${s}" '~1')" == 'racer rewrite after push' ]] ||
	fail 'verification must judge the tip that moved after the push' "${s}/out"
if tip_cask "${s}" | grep -q BAD; then
	fail 'the tip that moved after the push must end clean' "${s}/out"
fi

# A refused push that is not a race fails at once, with the reason in the log.
s="${tmp_dir}/refused"
new_remote "${s}" 'cask BAD'
cat >"${s}/remote.git/hooks/pre-receive" <<'EOF'
#!/usr/bin/env bash
count=0
if [[ -f refused-count ]]; then
	count="$(<refused-count)"
fi
printf '%s\n' "$((count + 1))" >refused-count
echo 'Permission to devantler-tech/homebrew-tap.git denied to release-bot.' >&2
exit 1
EOF
chmod +x "${s}/remote.git/hooks/pre-receive"
run_subject "${s}"
[[ "${run_status}" -eq 1 ]] || fail 'a refused push must fail' "${s}/out"
[[ "$(<"${s}/remote.git/refused-count")" -eq 1 ]] || fail 'a refused push must not be retried' "${s}/out"
grep -Fq 'not a branch race, so not retried' "${s}/out" || fail 'a refused push must be classified' "${s}/out"
grep -Fq 'denied' "${s}/out" || fail 'a refused push must carry the remote'\''s reason' "${s}/out"

# Offenses autocorrection cannot fix are never reported clean.
s="${tmp_dir}/stuck"
new_remote "${s}" 'cask STUCK'
run_subject "${s}"
[[ "${run_status}" -eq 1 ]] || fail 'remaining offenses must fail' "${s}/out"
grep -Fq 'still has brew style offenses' "${s}/out" || fail 'remaining offenses must be named' "${s}/out"

# A clone failure never prints the token embedded in the remote URL.
s="${tmp_dir}/clone-failure"
mkdir -p "${s}"
set +e
"${subject}" --remote 'https://x-access-token:s3cr3t-fixture-token@127.0.0.1:9/tap.git' \
	--branch "${branch}" --cask-name ksail >"${s}/out" 2>&1
run_status=$?
set -e
[[ "${run_status}" -eq 1 ]] || fail 'a clone failure must fail' "${s}/out"
grep -Fq 'Could not clone' "${s}/out" || fail 'a clone failure must be named' "${s}/out"
if grep -Fq 's3cr3t-fixture-token' "${s}/out"; then
	fail 'the remote URL token must be redacted' "${s}/out"
fi

# Without brew the script refuses rather than reporting an unchecked cask clean.
s="${tmp_dir}/no-brew"
new_remote "${s}" 'cask GOOD'
set +e
PATH="/usr/bin:/bin" "${subject}" --remote "${s}/remote.git" --branch "${branch}" --cask-name ksail >"${s}/out" 2>&1
run_status=$?
set -e
[[ "${run_status}" -eq 1 ]] || fail 'a missing brew must fail' "${s}/out"
grep -Fq 'brew is not available' "${s}/out" || fail 'a missing brew must be named' "${s}/out"

# Usage errors.
for args in '--branch b --cask-name ksail' '--remote r --branch b --cask-name ksail --attempts 0' '--remote'; do
	set +e
	# shellcheck disable=SC2086 # Word splitting builds the argument list on purpose.
	"${subject}" ${args} >/dev/null 2>&1
	run_status=$?
	set -e
	[[ "${run_status}" -eq 2 ]] || fail "usage error expected for: ${args}"
done

printf 'Cask style-clean branch suite passed.\n'
