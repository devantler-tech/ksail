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

The REST budget read does not spend the primary allowance but can hit secondary limits. The minimal GraphQL query costs one point and
reads that endpoint's own allowance, which can differ from REST's metadata.
Each request is limited to 10 seconds, with a 2-second forced-stop grace period.
GNU timeout (timeout on Linux or gtimeout on macOS) bounds the reads; without
it, the budget is treated as unreadable rather than issuing an unbounded request.

Exit codes:
  0  budget is available, or the budget could not be read (see below)
  1  a known rate limit cannot recover safely within the retry or wait bounds
  2  usage error

An unreadable budget is reported as a warning and exits 0. This script only
improves availability; the step after it still fails closed on its own
errors, so a broken budget read must not stop a release by itself. A known secondary
restriction is held until a complete budget read confirms recovery.

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

api_timeout=timeout
if ! command -v "${api_timeout}" >/dev/null 2>&1; then
	api_timeout=gtimeout
fi

# Bound each metadata request even when the HTTP client has no request timeout.
read_api() {
	command -v "${api_timeout}" >/dev/null 2>&1 || return 1
	"${api_timeout}" -k 2s 10s gh api "$@"
}

# Read authoritative headers from a complete response without printing private metadata.
# Receipts are a budget, a secondary backoff, or a known restriction we cannot validate.
read_api_budget() {
	local resource="$1" response status=0
	shift
	response="$(read_api "$@" 2>/dev/null)" || status=$?
	jq -eRcs --arg resource "$resource" --argjson status "$status" '
      def whole: type == "number" and . >= 0 and . <= 9007199254740991 and . == floor;
      def decimal: test("^(0|[1-9][0-9]*)$") and (tonumber | whole);
      capture("^(?<headers>.*?)\\r?\\n\\r?\\n(?<body>.*)$"; "p")
      | .body |= fromjson
      | if (.body | type) == "object" and ($status == 0 or $status == 1) then .
        else error("incomplete response") end
      | (.headers | split("\n") | map(sub("\r$"; ""))) as $lines
      | ($lines[0] | capture("^HTTP/[0-9.]+ (?<code>[0-9]{3})( |$)").code | tonumber) as $code
      | ($lines[1:] | map(select(test("^(x-ratelimit-(resource|remaining|reset)|retry-after):"; "i"))
          | capture("^(?<key>[^:]+):[ \\t]*(?<value>.*)$")
          | .key |= ascii_downcase)) as $headers
      | ($headers | from_entries) as $quota
      | ([$headers[] | select(.key == "retry-after")]) as $retry
      | ([.body.message // "", (.body.errors // [] | .[]? | .message // "")] | join(" ")
          | test("secondary rate limit|abuse detection"; "i")) as $secondary_message
      | if (($code == 403 or $code == 429) and ($code == 429 or $secondary_message or ($retry | length) > 0))
          or ($code == 200 and $status == 1 and $secondary_message) then
          # A malformed advertised backoff is held, never treated as an unreadable budget.
          if ($retry | length) > 1
              or (($retry | length) == 1 and ($retry[0].value | test("^(0|[1-9][0-9]{0,8})$") | not))
            then ["blocked"]
          elif $quota["x-ratelimit-remaining"] == "0" then
            if all(["x-ratelimit-resource", "x-ratelimit-remaining", "x-ratelimit-reset"][];
                . as $key | [$headers[] | select(.key == $key)] | length == 1)
                and $quota["x-ratelimit-resource"] == $resource
                and ($quota["x-ratelimit-reset"] | decimal and tonumber > 0)
              then ["secondary", (($retry[0].value // "60") | tonumber), ($quota["x-ratelimit-reset"] | tonumber)]
              else ["blocked"] end
          else ["secondary", (($retry[0].value // "60") | tonumber), 0] end
        else
          if all(["x-ratelimit-resource", "x-ratelimit-remaining", "x-ratelimit-reset"][];
              . as $key | [$headers[] | select(.key == $key)] | length == 1)
              and $quota["x-ratelimit-resource"] == $resource
              and all([$quota["x-ratelimit-remaining"], $quota["x-ratelimit-reset"]][]; decimal)
              and ($quota["x-ratelimit-reset"] | tonumber) > 0 then .
            else error("invalid quota headers") end
          | if ($code == 200 and $status == 0
                and (if $resource == "core" then
                    (.body.resources.core.remaining | whole) and (.body.resources.core.reset | whole and . > 0)
                  else (.body.errors // [] | length) == 0
                    and (.body.data.rateLimit.remaining | whole)
                    and (.body.data.rateLimit.resetAt | type) == "string" end))
              or ($status == 1 and $quota["x-ratelimit-remaining"] == "0"
                and (if $resource == "core" then $code == 403 or $code == 429
                  else $code == 200 and .body.data == null
                    and (.body.errors | type) == "array" and (.body.errors | length) > 0
                    and all(.body.errors[]; .type == "RATE_LIMIT" or .type == "RATE_LIMITED") end))
            then ["budget", ($quota["x-ratelimit-remaining"] | tonumber), ($quota["x-ratelimit-reset"] | tonumber)]
            else error("failed quota observation") end
        end
    ' <<<"$response" 2>/dev/null
}

# Do not issue the second request while the first endpoint asks us to back off.
read_budget() {
	local rest graphql
	rest="$(read_api_budget core --include rate_limit)" || return 1
	if [[ "$(jq -r '.[0]' <<<"$rest")" != budget ]]; then
		jq -r 'map(tostring) | join(" ")' <<<"$rest"
		return
	fi
	if ! graphql="$(read_api_budget graphql --include graphql -f 'query=query { rateLimit { remaining resetAt } }')"; then
		# A failed second read cannot erase the first endpoint's known restriction.
		if jq -e --argjson min_core "$min_core" '.[1] < $min_core' <<<"$rest" >/dev/null; then
			jq -r '["primary"] + .[1:] | map(tostring) | join(" ")' <<<"$rest"
			return
		fi
		return 1
	fi
	if [[ "$(jq -r '.[0]' <<<"$graphql")" != budget ]]; then
		jq -r --argjson rest "$rest" --argjson min_core "$min_core" '
          if .[0] == "secondary" and $rest[1] < $min_core
            then .[2] = ([.[2], $rest[2]] | max) else . end
          | map(tostring) | join(" ")
        ' <<<"$graphql"
		return
	fi
	jq -nr --argjson rest "$rest" --argjson graphql "$graphql" \
		'$rest + $graphql[1:] | map(tostring) | join(" ")'
}

started="$(date +%s)"
deadline=$((started + max_wait))
unreadable=0
secondary_attempts=0
secondary_backoff=0
recovering=0

while true; do
	if ! budget="$(read_budget)"; then
		if [[ "$recovering" -eq 1 ]]; then
			printf '::error::Could not confirm recovery from the GitHub secondary rate limit; holding release delivery.\n' >&2
			exit 1
		fi
		unreadable=$((unreadable + 1))
		# Give up after three attempts, or sooner when another retry would outlast --max-wait.
		if [[ "$unreadable" -ge 3 || $(($(date +%s) + 10)) -gt "$deadline" ]]; then
			printf '::warning::Could not read the GitHub request budget (%s attempt(s)); continuing without waiting.\n' \
				"$unreadable"
			exit 0
		fi
		sleep 10
		continue
	fi
	unreadable=0
	read -r kind core_remaining core_reset graphql_remaining graphql_reset <<<"$budget"

	case "$kind" in
	primary)
		now="$(date +%s)"
		pause=$((core_reset - now + 5))
		[[ "$pause" -lt 5 ]] && pause=5
		if [[ $((now + pause)) -gt "$deadline" ]]; then
			printf '::error::GitHub REST request budget is too low (%s) and does not return within %s s.\n' "$core_remaining" "$max_wait" >&2
			exit 1
		fi
		printf '::notice::GitHub REST request budget is low (%s); GraphQL is unreadable; waiting %s s for the known reset.\n' "$core_remaining" "$pause"
		sleep "$pause"
		continue
		;;
	blocked)
		printf '::error::Could not validate GitHub rate-limit backoff; holding release delivery.\n' >&2
		exit 1
		;;
	secondary)
		recovering=1
		secondary_attempts=$((secondary_attempts + 1))
		if [[ "$secondary_attempts" -ge 3 ]]; then
			printf '::error::GitHub secondary rate limit persisted after three attempts; holding release delivery.\n' >&2
			exit 1
		fi
		now="$(date +%s)"
		pause="$core_remaining"
		[[ "$pause" -lt "$secondary_backoff" ]] && pause="$secondary_backoff"
		[[ "$pause" -lt 1 ]] && pause=1
		# A simultaneous primary exhaustion must also reach its reset window.
		if [[ "$core_reset" -gt 0 && $((core_reset - now + 5)) -gt "$pause" ]]; then
			pause=$((core_reset - now + 5))
		fi
		if [[ $((now + pause)) -gt "$deadline" ]]; then
			printf '::error::GitHub secondary rate limit does not recover within %s s; holding release delivery.\n' "$max_wait" >&2
			exit 1
		fi
		printf '::notice::GitHub secondary rate limit; waiting %s s before another budget read.\n' "$pause"
		sleep "$pause"
		secondary_backoff=$((pause * 2))
		continue
		;;
	budget)
		secondary_attempts=0
		secondary_backoff=0
		recovering=0
		;;
	*)
		printf '::error::Invalid GitHub budget observation; holding release delivery.\n' >&2
		exit 1
		;;
	esac

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
