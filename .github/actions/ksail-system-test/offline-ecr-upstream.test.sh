#!/usr/bin/env bash
# The offline VCluster leg must give both the mirror and containerd a reachable
# local registry that has no ECR Public image data.
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
subject="$action_dir/offline-ecr-upstream.sh"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"

cat >"$fixture/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2 $3" in
  'network inspect bridge')
    echo 172.17.0.1
    ;;
  'run -d --name')
    name=$4
    shift 4
    port=''
    while [ "$#" -gt 0 ]; do
      if [ "$1" = '-p' ]; then port=$2; fi
      shift
    done
    printf '%s|%s\n' "$name" "$port" >"$FAKE_DOCKER_STATE"
    echo test-container-id
    ;;
  'rm -f '*)
    [ -f "$FAKE_DOCKER_STATE" ]
    [ "$(cut -d'|' -f1 "$FAKE_DOCKER_STATE")" = "$3" ]
    rm "$FAKE_DOCKER_STATE"
    ;;
  *)
    echo "unexpected Docker call: $*" >&2
    exit 1
    ;;
esac
SH
cat >"$fixture/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
url="http:"'//172.17.0.1:5505/v2/'
[ "$*" = "-fsS $url" ]
[ -f "$FAKE_DOCKER_STATE" ]
[ "$(cut -d'|' -f2 "$FAKE_DOCKER_STATE")" = '172.17.0.1:5505:5000' ]
SH
cat >"$fixture/bin/sleep" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "$fixture/bin/docker" "$fixture/bin/curl" "$fixture/bin/sleep"
offline_url="http:"'//172.17.0.1:5505'

if [ ! -f "$subject" ]; then
	echo 'FAIL: offline ECR upstream helper is missing' >&2
	exit 1
fi

PATH="$fixture/bin:$PATH" FAKE_DOCKER_STATE="$fixture/docker-state" \
	GITHUB_OUTPUT="$fixture/output" GITHUB_RUN_ID=17 GITHUB_RUN_ATTEMPT=2 \
	bash "$subject" start

grep -Fxq "remote-url=$offline_url" "$fixture/output" || {
	echo 'FAIL: fallback did not target the empty local registry' >&2
	exit 1
}
grep -Fxq 'container-name=ksail-ecr-offline-17-2' "$fixture/output" || {
	echo 'FAIL: cleanup identity was not exported' >&2
	exit 1
}
grep -Fxq 'ksail-ecr-offline-17-2|172.17.0.1:5505:5000' "$fixture/docker-state" || {
	echo 'FAIL: the empty registry was not reachable through the host bridge' >&2
	exit 1
}

PATH="$fixture/bin:$PATH" FAKE_DOCKER_STATE="$fixture/docker-state" \
	bash "$subject" stop ksail-ecr-offline-17-2
[ ! -e "$fixture/docker-state" ] || {
	echo 'FAIL: the empty registry survived cleanup' >&2
	exit 1
}

echo 'PASS: offline ECR fallback uses a reachable empty local registry'
