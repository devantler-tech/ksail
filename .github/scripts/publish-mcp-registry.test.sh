#!/usr/bin/env bash
# Exercise the actual CD authentication/publication commands without registry writes.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}
yq -r '.jobs.changes.steps[] | select(.id=="filter") | .with.filters' \
	"$root/.github/workflows/ci.yaml" >"$work/ci-filters.yaml"
yq -o=json '."cask-pr-handoff"' "$work/ci-filters.yaml" |
	jq -e 'index("server.json") != null' >/dev/null ||
	fail 'manifest-only changes must select MCP publication controls'

yq -r '.jobs["mcp-publish"].steps[-2].run' "$root/.github/workflows/cd.yaml" >"$work/auth.sh"
yq -r '.jobs["mcp-publish"].steps[-1].run' "$root/.github/workflows/cd.yaml" >"$work/publish.sh"
yq -o=json '.jobs["mcp-publish"]' "$root/.github/workflows/cd.yaml" |
	jq -e '.permissions=={"id-token":"write","contents":"read"} and
    .steps[-2].run=="./mcp-publisher login github-oidc" and
    all(.steps[-2:][]; .if==null and .["continue-on-error"]==null)' >/dev/null ||
	fail 'CD must authenticate before unconditional publication with unchanged permissions'

cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CASE_DIR/reads"
output=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output|-o) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ "${READ_FAILURE:-0}" = 0 ] || exit 28
[ -n "$output" ] || exit 2
cp "$CASE_DIR/record.json" "$output"
printf '%s' "$HTTP_STATUS"
STUB
cat >"$work/bin/sleep" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$1" >>"$CASE_DIR/delays"
STUB
cat >"$work/publisher" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'login github-oidc')
    printf 'login\n' >>"$CASE_DIR/auth"
    [ "${AUTH_FAILURE:-0}" = 0 ]
    ;;
  publish)
    printf 'publish\n' >>"$CASE_DIR/writes"
    count="$(wc -l <"$CASE_DIR/writes" | tr -d '[:space:]')"
    case "$PUBLISH_RESULT" in
      success) exit 0 ;;
      transient) [ "$count" -ge 3 ] && exit 0 ;;
      exhausted) : ;;
      duplicate) echo 'Error: publish failed: server returned status 400: version already exists' >&2; exit 1 ;;
      unauthorized) echo 'Error: publish failed: server returned status 401: unauthorized' >&2; exit 1 ;;
      forbidden) echo 'Error: publish failed: server returned status 403: forbidden' >&2; exit 1 ;;
      client) echo 'Error: publish failed: server returned status 400: invalid package metadata' >&2; exit 1 ;;
      client-body-5xx) echo 'Error: publish failed: server returned status 400: upstream mentioned status 503' >&2; exit 1 ;;
      *) exit 2 ;;
    esac
    echo 'Error: publish failed: server returned status 503: temporarily unavailable' >&2
    exit 1
    ;;
  *) exit 2 ;;
esac
STUB
chmod +x "$work/bin/curl" "$work/bin/sleep" "$work/publisher"

# Prepare the same versioned manifest CD uses, then build independent conflicting records.
prepare() {
	case_dir="$work/$1"
	mkdir -p "$case_dir/.github/scripts"
	cp "$work/publisher" "$case_dir/mcp-publisher"
	if [ -f "$root/.github/scripts/publish-mcp-registry.sh" ]; then
		cp "$root/.github/scripts/publish-mcp-registry.sh" "$case_dir/.github/scripts/"
	fi
	jq '.version="7.204.5" | .packages[0].identifier="ghcr.io/devantler-tech/ksail:v7.204.5"' \
		"$root/server.json" >"$case_dir/server.json"
	jq -n --slurpfile server "$case_dir/server.json" \
		'{server:$server[0],_meta:{"io.modelcontextprotocol.registry/official":{status:"active"}}}' >"$case_dir/record.json"
	: >"$case_dir/writes"
	: >"$case_dir/reads"
	: >"$case_dir/auth"
	: >"$case_dir/delays"
}

run_case() {
	local name="$1" expected="$2" expected_writes="$3" status=0 writes
	(
		cd "$case_dir"
		export CASE_DIR="$case_dir" PATH="$work/bin:$PATH"
		export HTTP_STATUS="${HTTP_STATUS:-200}" PUBLISH_RESULT="${PUBLISH_RESULT:-duplicate}"
		export READ_FAILURE="${READ_FAILURE:-0}" AUTH_FAILURE="${AUTH_FAILURE:-0}"
		bash --noprofile --norc -e -o pipefail "$work/auth.sh" &&
			bash --noprofile --norc -e -o pipefail "$work/publish.sh"
	) >"$case_dir/output" 2>&1 || status=$?
	writes="$(wc -l <"$case_dir/writes" | tr -d '[:space:]')"
	if { [ "$expected" = success ] && [ "$status" -ne 0 ]; } ||
		{ [ "$expected" = failure ] && [ "$status" -eq 0 ]; } ||
		[ "$writes" -ne "$expected_writes" ]; then
		cat "$case_dir/output" >&2
		fail "$name: expected $expected with $expected_writes writes, got exit $status with $writes"
	fi
	printf 'PASS: %s\n' "$name"
	unset HTTP_STATUS PUBLISH_RESULT READ_FAILURE AUTH_FAILURE
}

prepare identical
run_case identical success 0
grep -Fq 'https://registry.modelcontextprotocol.io/v0.1/servers/io.github.devantler-tech%2Fksail/versions/7.204.5' "$case_dir/reads" || fail 'exact version was not read'
prepare reordered
jq -S '._meta["io.modelcontextprotocol.registry/official"].publishedAt="2026-10-21T00:00:00Z"' "$case_dir/record.json" >"$case_dir/changed.json"
mv "$case_dir/changed.json" "$case_dir/record.json"
run_case reordered success 0
for variant in inactive missing-status wrong-version wrong-package extra-field missing-field multiple-records malformed; do
	prepare "$variant"
	case "$variant" in
	inactive) change='._meta["io.modelcontextprotocol.registry/official"].status="deleted"' ;;
	missing-status) change='del(._meta)' ;;
	wrong-version) change='.server.version="7.204.6"' ;;
	wrong-package) change='.server.packages[0].identifier="ghcr.io/devantler-tech/ksail:v7.204.6"' ;;
	extra-field) change='.server.extra="different"' ;;
	missing-field) change='del(.server.description)' ;;
	multiple-records) cat "$case_dir/record.json" "$case_dir/record.json" >"$case_dir/changed.json" ;;
	malformed) printf '{invalid' >"$case_dir/changed.json" ;;
	esac
	if [ "$variant" != multiple-records ] && [ "$variant" != malformed ]; then
		jq "$change" "$case_dir/record.json" >"$case_dir/changed.json"
	fi
	mv "$case_dir/changed.json" "$case_dir/record.json"
	run_case "$variant" failure 0
done
for response in 201 302 401 403 500; do
	prepare "http-$response"
	HTTP_STATUS="$response" run_case "http-$response" failure 0
done
prepare unreadable
READ_FAILURE=1 run_case unreadable failure 0
prepare auth-failure
AUTH_FAILURE=1 run_case auth-failure failure 0
[ ! -s "$case_dir/reads" ] || fail 'publication ran after authentication failed'
prepare new-version
HTTP_STATUS=404 PUBLISH_RESULT=success run_case new-version success 1
prepare transient
HTTP_STATUS=404 PUBLISH_RESULT=transient run_case transient success 3
printf '15\n30\n' >"$work/expected-delays"
cmp -s "$work/expected-delays" "$case_dir/delays" || fail 'server retries lost their bounded backoff'
prepare exhausted
HTTP_STATUS=404 PUBLISH_RESULT=exhausted run_case exhausted failure 3
for result in unauthorized forbidden client client-body-5xx duplicate; do
	prepare "$result"
	HTTP_STATUS=404 PUBLISH_RESULT="$result" run_case "$result" failure 1
done
for variant in wrong-name empty-version multiple-manifests; do
	prepare "$variant"
	case "$variant" in
	wrong-name) jq '.name="io.github.another/server"' "$case_dir/server.json" >"$case_dir/changed.json" ;;
	empty-version) jq '.version=""' "$case_dir/server.json" >"$case_dir/changed.json" ;;
	multiple-manifests) cat "$case_dir/server.json" "$case_dir/server.json" >"$case_dir/changed.json" ;;
	esac
	mv "$case_dir/changed.json" "$case_dir/server.json"
	run_case "$variant" failure 0
	[ ! -s "$case_dir/reads" ] || fail "$variant reached the registry"
done
printf 'PASS: real MCP CD publication and refusal controls\n'
