#!/usr/bin/env bash

set -euo pipefail

# Print the release wait options and the behavior of each exit status.
usage() {
	cat <<'EOF'
Usage:
  wait-for-github-api-budget.sh [--min-core N] [--min-graphql N] [--max-wait SECONDS]

Release pre-step: wait until the token in GH_TOKEN has request budget left
before a step that spends it. The tap token belongs to an account whose hourly
budget is shared with other automation, so a release can start while that
budget is at zero. Without this wait the next step fails with "API rate limit
exceeded" and the release stops half-way (devantler-tech/monorepo#4055).

The REST budget read is free. The minimal GraphQL query costs one point and
reads that endpoint's own allowance, which can differ from REST's metadata.

Exit codes:
  0  budget is available, or the budget could not be read (see below)
  1  budget is too low and does not return within --max-wait
  2  usage error

An unreadable budget is reported as a warning and exits 0. This script only
improves availability; the step after it still fails closed on its own
errors, so a broken budget read must not stop a release by itself.

Defaults: --min-core 300, --min-graphql 50, --max-wait 3700 (GitHub's window
is one hour, so a full wait always reaches the reset).
EOF
}

min_core=300
min_graphql=50
max_wait=3700

while [[ $# -gt 0 ]]; do
	case "$1" in
	--help | -h)
		usage
		exit 0
		;;
	--min-core | --min-graphql | --max-wait)
		if [[ $# -lt 2 || ! "$2" =~ ^(0|[1-9][0-9]{0,8})$ ]]; then
			printf 'ERROR: %s needs a whole number\n' "$1" >&2
			exit 2
		fi
		case "$1" in
		--min-core) min_core="$2" ;;
		--min-graphql) min_graphql="$2" ;;
		--max-wait) max_wait="$2" ;;
		esac
		shift 2
		;;
	*)
		printf 'ERROR: unknown argument %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

# Read GraphQL's own response headers, including a complete primary-limit refusal.
# Other failures and incomplete responses remain unreadable, without printing headers.
read_graphql_budget() {
	local response status=0
	response="$(gh api --include graphql -f 'query=query { rateLimit { remaining resetAt } }' 2>/dev/null)" || status=$?
	jq -eRcs --argjson status "${status}" '
      capture("^(?<headers>.*?)\\r?\\n\\r?\\n(?<body>.*)$"; "p")
      | .body |= fromjson
      | (.headers | split("\n") | map(sub("\r$"; ""))) as $lines
      | if ($lines[0] | test("^HTTP/[0-9.]+ 200 ")) then .
        else error("not a complete GraphQL response") end
      | ($lines[1:] | map(select(test("^x-ratelimit-(resource|remaining|reset):"; "i"))
          | capture("^(?<key>[^:]+):[ \\t]*(?<value>.*)$")
          | .key |= ascii_downcase)) as $headers
      | (if all(["x-ratelimit-resource", "x-ratelimit-remaining", "x-ratelimit-reset"][];
          . as $key | [$headers[] | select(.key == $key)] | length == 1)
        then ($headers | map({key, value}) | from_entries)
        else error("missing or duplicate quota headers") end) as $quota
      | if $quota["x-ratelimit-resource"] == "graphql"
          and all([$quota["x-ratelimit-remaining"], $quota["x-ratelimit-reset"]][];
            test("^(0|[1-9][0-9]*)$")) then .
        else error("invalid quota headers") end
      | if ($status == 0 and (.body.errors // [] | length) == 0
            and (.body.data.rateLimit.remaining | type) == "number"
            and (.body.data.rateLimit.resetAt | type) == "string")
          or ($status == 1 and .body.data == null
            and (.body.errors | type) == "array" and (.body.errors | length) > 0
            and all(.body.errors[]; .type == "RATE_LIMIT")
            and $quota["x-ratelimit-remaining"] == "0")
        then [$quota["x-ratelimit-remaining"], $quota["x-ratelimit-reset"]] | map(tonumber)
        else error("failed or incomplete quota observation") end
    ' <<<"${response}" 2>/dev/null
}

# Prints "<core remaining> <core reset> <graphql remaining> <graphql reset>", or fails when the
# response does not contain bounded whole numbers and positive reset timestamps.
read_budget() {
	local response graphql fields
	if ! response="$(gh api rate_limit 2>/dev/null)"; then
		return 1
	fi
	if ! graphql="$(read_graphql_budget)"; then
		return 1
	fi
	# Keep integers within the exact JSON range and the Bash arithmetic range.
	# A zero reset cannot identify the exhausted resource's next window.
	if ! fields="$(jq -er --argjson graphql "${graphql}" '
      [.resources.core.remaining, .resources.core.reset] + $graphql
      | if all(.[]; type == "number" and . >= 0 and . <= 9007199254740991 and . == floor)
          and .[1] > 0 and .[3] > 0
        then map(tostring)
        else error("invalid budget numbers") end
      # Only canonical decimal strings may reach Bash numeric comparisons.
      | if all(.[]; test("^(0|[1-9][0-9]*)$"))
        then join(" ")
        else error("nondecimal budget numbers") end
    ' <<<"${response}" 2>/dev/null)"; then
		return 1
	fi
	printf '%s\n' "${fields}"
}

started="$(date +%s)"
deadline=$((started + max_wait))
unreadable=0

while true; do
	if ! budget="$(read_budget)"; then
		unreadable=$((unreadable + 1))
		# Give up after three attempts, or sooner when another retry would outlast --max-wait.
		if [[ "${unreadable}" -ge 3 || $(($(date +%s) + 10)) -gt "${deadline}" ]]; then
			printf '::warning::Could not read the GitHub request budget (%s attempt(s)); continuing without waiting.\n' \
				"${unreadable}"
			exit 0
		fi
		sleep 10
		continue
	fi
	unreadable=0
	read -r core_remaining core_reset graphql_remaining graphql_reset <<<"${budget}"

	wake=0
	if [[ "${core_remaining}" -lt "${min_core}" ]]; then
		wake="${core_reset}"
	fi
	if [[ "${graphql_remaining}" -lt "${min_graphql}" && "${graphql_reset}" -gt "${wake}" ]]; then
		wake="${graphql_reset}"
	fi
	if [[ "${wake}" -eq 0 ]]; then
		printf 'GitHub request budget is available: REST %s, GraphQL %s (waited %s s)\n' \
			"${core_remaining}" "${graphql_remaining}" "$(($(date +%s) - started))"
		exit 0
	fi

	now="$(date +%s)"
	# Five seconds past the reset, so the next read lands in the new window.
	pause=$((wake - now + 5))
	if [[ "${pause}" -lt 5 ]]; then
		pause=5
	fi
	if [[ $((now + pause)) -gt "${deadline}" ]]; then
		printf '::error::GitHub request budget is too low (REST %s, GraphQL %s) and does not return within %s s.\n' \
			"${core_remaining}" "${graphql_remaining}" "${max_wait}" >&2
		exit 1
	fi
	printf '::notice::GitHub request budget is low (REST %s of at least %s, GraphQL %s of at least %s); waiting %s s for the reset.\n' \
		"${core_remaining}" "${min_core}" "${graphql_remaining}" "${min_graphql}" "${pause}"
	sleep "${pause}"
done
