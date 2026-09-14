#!/usr/bin/env bash
# style-clean-cask-branch.sh — leave a generated cask on a tap branch brew-style-clean.
#
# GoReleaser's cask template emits `brew style` offenses no config knob can reach, and the tap's
# own CI autocorrects the same branch on every push. Both writers race: when the tap lands its
# autocorrection first, a plain push here is rejected as non-fast-forward even though the branch
# is already clean (#6628). So a non-fast-forward rejection re-fetches the branch tip and re-checks
# it, while any other push failure stops immediately with its reason named.
#
# Usage:
#   style-clean-cask-branch.sh --remote <url> --branch <branch> --cask-name <name> [--attempts <n>]
#
# Exit codes:
#   0  the branch tip on the remote passes `brew style` (autocorrections pushed, or already clean)
#   1  it could not be made clean (clone/fetch failure, a refused push, remaining offenses, or the
#      branch kept moving for every attempt)
#   2  usage error

set -euo pipefail

usage() {
	printf 'usage: %s --remote <url> --branch <branch> --cask-name <name> [--attempts <n>]\n' "$0" >&2
	exit 2
}

remote=""
branch=""
name=""
attempts=3
while [[ $# -gt 0 ]]; do
	case "$1" in
	--remote | --branch | --cask-name | --attempts)
		[[ $# -ge 2 ]] || usage
		case "$1" in
		--remote) remote="$2" ;;
		--branch) branch="$2" ;;
		--cask-name) name="$2" ;;
		--attempts) attempts="$2" ;;
		esac
		shift 2
		;;
	*) usage ;;
	esac
done
[[ -n "${remote}" && -n "${branch}" && -n "${name}" ]] || usage
[[ "${attempts}" =~ ^[1-9][0-9]*$ ]] || usage

# The remote URL can carry a token, and git echoes URLs in its errors.
redact() {
	sed -E 's#(https?://)[^/@[:space:]]+@#\1#g'
}

# Git puts the actual cause (a `remote:` message, the rejected ref) before its closing
# "failed to push some refs" line, so keep every meaningful line, not just the last one.
reason() {
	redact <"$1" | grep -vE '^[[:space:]]*$|^hint:' | paste -sd ';' - || true
}

if ! command -v brew >/dev/null 2>&1; then
	printf '::error::brew is not available on this runner; cannot style-check %s\n' "${name}"
	exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
tap="${work}/tap"
cask="${tap}/Casks/${name}.rb"

if ! git clone --quiet --depth 1 --branch "${branch}" "${remote}" "${tap}" 2>"${work}/clone.err"; then
	printf '::warning::Could not clone branch %s for brew style --fix: %s\n' "${branch}" "$(reason "${work}/clone.err")"
	exit 1
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
	# `brew style --fix` can apply partial autocorrections yet still exit non-zero when offenses
	# remain, so commit whatever diff it produced and let the read-only check below decide.
	brew style --fix "${cask}" ||
		printf '::warning::brew style --fix exited non-zero for %s; committing any partial autocorrections\n' "${name}"

	if git -C "${tap}" diff --quiet; then
		printf '%s cask has no autocorrectable diff to push\n' "${name}"
	else
		git -C "${tap}" \
			-c user.name='github-actions[bot]' \
			-c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
			commit --quiet -am 'style: brew style --fix generated cask'

		if git -C "${tap}" push --quiet origin "HEAD:refs/heads/${branch}" 2>"${work}/push.err"; then
			printf 'Pushed brew style --fix autocorrections to %s\n' "${branch}"
		elif grep -qiE 'fetch first|non-fast-forward' "${work}/push.err"; then
			printf '::warning::Push to %s was rejected as non-fast-forward: another writer moved the branch (attempt %d of %d); re-checking its new tip\n' \
				"${branch}" "${attempt}" "${attempts}"
			if ! git -C "${tap}" fetch --quiet --depth 1 origin "refs/heads/${branch}" 2>"${work}/fetch.err"; then
				printf '::warning::Could not re-fetch %s after the rejected push: %s\n' "${branch}" "$(reason "${work}/fetch.err")"
				exit 1
			fi
			# Drop this run's local commit: the fresh tip is re-checked from scratch.
			git -C "${tap}" checkout --quiet --force -B "${branch}" FETCH_HEAD
			continue
		else
			printf '::warning::Could not push brew style --fix autocorrections to %s (not a branch race, so not retried): %s\n' \
				"${branch}" "$(reason "${work}/push.err")"
			exit 1
		fi
	fi

	# Verify the tip the remote holds now, not the one this run last saw: another writer can move
	# the branch after the clone or right after this run's push.
	if ! git -C "${tap}" fetch --quiet --depth 1 origin "refs/heads/${branch}" 2>"${work}/fetch.err"; then
		printf '::warning::Could not fetch %s for final verification: %s\n' "${branch}" "$(reason "${work}/fetch.err")"
		exit 1
	fi
	if [[ "$(git -C "${tap}" rev-parse HEAD)" != "$(git -C "${tap}" rev-parse FETCH_HEAD)" ]]; then
		printf '::warning::%s moved before verification (attempt %d of %d); re-checking its new tip\n' \
			"${branch}" "${attempt}" "${attempts}"
		git -C "${tap}" checkout --quiet --force -B "${branch}" FETCH_HEAD
		continue
	fi

	# A pushed fix (or an empty diff) does not prove the cask is clean: only a read-only pass does.
	if brew style "${cask}"; then
		printf '%s cask is brew-style-clean\n' "${name}"
		exit 0
	fi
	printf '::warning::%s cask still has brew style offenses after autocorrection\n' "${name}"
	exit 1
done

printf '::warning::%s kept moving under this job for %d attempts; could not confirm a brew-style-clean tip\n' \
	"${branch}" "${attempts}"
exit 1
