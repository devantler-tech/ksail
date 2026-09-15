#!/usr/bin/env bash

set -euo pipefail

# The retry budget the publish-release job in cd.yaml must fit inside its timeout. The workflow suite
# reads these two lines, so keep them as plain assignments.
DEFAULT_ATTEMPTS=5
DEFAULT_DELAY_SECONDS=15

# usage prints the command-line contract.
usage() {
	cat <<'EOF'
Usage:
  upload-release-assets.sh --tag TAG --assets-dir DIR [--repo OWNER/REPO]
                           [--attempts N] [--delay-seconds S]

Attach every file in DIR to the draft release TAG with `gh release upload --clobber`,
retrying a failed upload with exponential backoff (S seconds, doubling).

  - A missing DIR, or one holding no files, fails immediately: a wrong path is a
    build bug, not a transient, and must not burn the retry budget.
  - A success after a retry says which attempt succeeded.
  - Giving up says that nothing was published and the release is still a draft.

--clobber makes a retry replace a partially uploaded asset instead of failing on
its name. --repo defaults to $GH_REPO, then $GITHUB_REPOSITORY. Defaults: 5
attempts, 15 seconds.
EOF
}

# require_value rejects a value-taking option given as the last argument, so it fails as an argument
# error (status 2) instead of `shift 2` aborting the script under `set -e` with status 1.
require_value() {
	if (($# < 2)); then
		printf 'ERROR: %s needs a value\n' "$1" >&2
		usage >&2
		exit 2
	fi
}

tag=""
repo="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
assets_dir=""
attempts="${DEFAULT_ATTEMPTS}"
delay="${DEFAULT_DELAY_SECONDS}"

while (($# > 0)); do
	case "$1" in
	--tag)
		require_value "$@"
		tag="$2"
		shift 2
		;;
	--repo)
		require_value "$@"
		repo="$2"
		shift 2
		;;
	--assets-dir)
		require_value "$@"
		assets_dir="$2"
		shift 2
		;;
	--attempts)
		require_value "$@"
		attempts="$2"
		shift 2
		;;
	--delay-seconds)
		require_value "$@"
		delay="$2"
		shift 2
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		printf 'ERROR: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ -z "${tag}" || -z "${repo}" || -z "${assets_dir}" ]]; then
	printf 'ERROR: --tag, --assets-dir and a repository (--repo or GH_REPO) are required\n' >&2
	usage >&2
	exit 2
fi
if ! [[ "${attempts}" =~ ^[1-9][0-9]?$ ]]; then
	printf 'ERROR: --attempts must be an integer from 1 to 99, got %q\n' "${attempts}" >&2
	exit 2
fi
if ! [[ "${delay}" =~ ^(0|[1-9][0-9]{0,2})$ ]]; then
	printf 'ERROR: --delay-seconds must be an integer from 0 to 999, got %q\n' "${delay}" >&2
	exit 2
fi

if [[ ! -d "${assets_dir}" ]]; then
	printf '::error::%s does not exist — the asset jobs produced nothing for this step to attach to %s.\n' \
		"${assets_dir}" "${tag}" >&2
	exit 1
fi
assets=()
for asset in "${assets_dir}"/*; do
	[[ -f "${asset}" ]] && assets+=("${asset}")
done
if ((${#assets[@]} == 0)); then
	printf '::error::%s holds no files — the asset jobs produced nothing for this step to attach to %s.\n' \
		"${assets_dir}" "${tag}" >&2
	exit 1
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
	if gh release upload "${tag}" "${assets[@]}" --repo "${repo}" --clobber; then
		# A retried success must not read like a clean first attempt to whoever scans this log later.
		if ((attempt > 1)); then
			printf 'uploaded %d asset(s) to %s on attempt %d/%d\n' "${#assets[@]}" "${tag}" "${attempt}" "${attempts}"
		fi
		exit 0
	fi
	if ((attempt < attempts)); then
		printf 'attempt %d/%d to upload %d asset(s) to %s failed; retrying in %ss\n' \
			"${attempt}" "${attempts}" "${#assets[@]}" "${tag}" "${delay}" >&2
		sleep "${delay}"
		delay=$((delay * 2))
	fi
done

# cd.yaml's cleanup-failed-release job deletes the orphaned draft after this failure, so say what happens next.
printf '::error::failed to attach %d asset(s) to %s after %d attempts. Nothing was published; %s is still a draft, which the cleanup job removes.\n' \
	"${#assets[@]}" "${tag}" "${attempts}" "${tag}" >&2
exit 1
