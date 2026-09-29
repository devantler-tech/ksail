#!/usr/bin/env bash
# A malformed readiness response must never pass the offline consumer gate.
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
subject="$action_dir/verify-offline-ecr-pull.sh"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"

cat >"$fixture/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  ps) echo vcluster.cp.test ;;
  exec)
    [ "$2" = vcluster.cp.test ]
    [ "$3" = grep ]
    [ "$4" = -Fxq ]
    upstream="http:"'//172.17.0.1:5505'
    [ "$5" = "server = \"$upstream\"" ]
    [ "$6" = /etc/containerd/certs.d/ecr-public.aws.com/hosts.toml ]
    ;;
  *) exit 1 ;;
esac
SH
cat >"$fixture/bin/ksail" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[ "$*" = "workload get deployment argocd-redis -n argocd -o jsonpath={.status.readyReplicas}" ]
printf '%s' "$FAKE_READY_REPLICAS"
SH
chmod +x "$fixture/bin/docker" "$fixture/bin/ksail"
offline_url="http:"'//172.17.0.1:5505'

if [ ! -f "$subject" ]; then
	echo 'FAIL: offline ECR consumer verifier is missing' >&2
	exit 1
fi

PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS=1 \
	bash "$subject" "$offline_url" >"$fixture/good-log"
grep -Fq 'Argo CD Redis is ready with local-only ECR fallback' "$fixture/good-log"

for invalid in 0 unknown; do
	if PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS="$invalid" \
		bash "$subject" "$offline_url" >"$fixture/bad-log" 2>&1; then
		echo "FAIL: readiness value '$invalid' was accepted" >&2
		exit 1
	fi
done

echo 'PASS: offline ECR consumer gate requires a ready Redis deployment'
