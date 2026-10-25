#!/usr/bin/env bash
# Called after mandatory OIDC login. A replay may reuse only the exact active record.
set -euo pipefail

version="$(jq -ers '
  if length==1 and .[0].name=="io.github.devantler-tech/ksail" and
    (.[0].version | type)=="string" and (.[0].version | length)>0
  then .[0].version | @uri
  else error("expected one versioned KSail server manifest") end
' server.json)"
response="$(mktemp)"
trap 'rm -f "$response"' EXIT
url="https://registry.modelcontextprotocol.io/v0.1/servers/io.github.devantler-tech%2Fksail/versions/$version"
if ! status="$(curl --disable --silent --show-error --proto '=https' \
	--connect-timeout 10 --max-time 30 --output "$response" --write-out '%{http_code}' "$url")"; then
	echo '::error::Could not read the exact MCP registry version; refusing ambiguous publication'
	exit 1
fi
case "$status" in
200)
	if ! jq -es --slurpfile intended server.json '
      length==1 and ($intended|length)==1 and
      .[0]._meta["io.modelcontextprotocol.registry/official"].status=="active" and
      .[0].server==$intended[0]
    ' "$response" >/dev/null; then
		echo '::error::Existing MCP version is inactive, unreadable or differs from the intended server metadata'
		exit 1
	fi
	echo 'Verified the exact active MCP registry record; publication already completed'
	exit 0
	;;
404) ;; # New immutable version: publish normally, without replacing another record.
*)
	echo "::error::MCP registry version read returned HTTP $status; refusing ambiguous publication"
	exit 1
	;;
esac

max_attempts=3
delay=15
for attempt in $(seq 1 "$max_attempts"); do
	echo "Publish attempt ${attempt}/${max_attempts}..."
	if last_output="$(./mcp-publisher publish 2>&1)"; then
		echo "$last_output"
		exit 0
	fi
	echo "$last_output"
	# v1.7.8 reports the actual HTTP status in this prefix. A 5xx mentioned in
	# a client-error body is not a retryable server response.
	publisher_status="$(sed -n 's/^Error: publish failed: server returned status \([0-9][0-9][0-9]\):.*/\1/p' <<<"$last_output")"
	if [[ "$publisher_status" =~ ^5[0-9]{2}$ ]] && [ "$attempt" -lt "$max_attempts" ]; then
		echo "Server error; retrying in ${delay}s..."
		sleep "$delay"
		delay=$((delay * 2))
	else
		break
	fi
done
echo "::error::mcp-publisher publish failed after ${attempt} attempt(s)"
exit 1
