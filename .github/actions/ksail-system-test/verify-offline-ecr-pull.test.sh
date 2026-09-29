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
  ps) echo vcluster.cp.vcluster-default ;;
  exec)
    [ "$2" = vcluster.cp.vcluster-default ]
    if [ "$3" = grep ]; then
      [ "$4" = -Fxq ]
      upstream="http:"'//172.17.0.1:5505'
      [ "$5" = "server = \"$upstream\"" ]
      [ "$6" = /etc/containerd/certs.d/ecr-public.aws.com/hosts.toml ]
    elif [ "$3" = containerd ]; then
      [ "$3 $4 $5" = 'containerd config dump' ]
      printf '[plugins."io.containerd.cri.v1.images".registry]\n  config_path = "%s"\n' "$FAKE_CONFIG_PATH"
    else
      [ "$3 $4 $5 $6" = 'crictl info -o json' ]
      printf '{"config":{"registry":{"configPath":"%s"}}}\n' "$FAKE_CRI_PATH"
    fi
    ;;
  logs)
    [ "$2" = vcluster-default-ecr-public.aws.com ]
    if [ "$FAKE_MIRROR_REQUESTS" = 1 ]; then
      echo 'http.request.method=GET http.request.uri="/v2/docker/library/redis/manifests/8.6.4-alpine" http.response.status=200'
    fi
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
	FAKE_CONFIG_PATH=/etc/containerd/certs.d FAKE_CRI_PATH=/etc/containerd/certs.d FAKE_MIRROR_REQUESTS=1 \
	bash "$subject" "$offline_url" >"$fixture/good-log"
grep -Fq 'Argo CD Redis is ready through the restored ECR mirror with local-only fallback' "$fixture/good-log"

for invalid in 0 unknown; do
	if PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS="$invalid" \
		FAKE_CONFIG_PATH=/etc/containerd/certs.d FAKE_CRI_PATH=/etc/containerd/certs.d FAKE_MIRROR_REQUESTS=1 \
		bash "$subject" "$offline_url" >"$fixture/bad-log" 2>&1; then
		echo "FAIL: readiness value '$invalid' was accepted" >&2
		exit 1
	fi
done

if PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS=1 \
	FAKE_CONFIG_PATH=/opt/unrelated-certs FAKE_CRI_PATH=/etc/containerd/certs.d FAKE_MIRROR_REQUESTS=1 \
	bash "$subject" "$offline_url" >"$fixture/bad-config-log" 2>&1; then
	echo 'FAIL: a missing effective CRI registry path was accepted' >&2
	exit 1
fi
if PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS=1 \
	FAKE_CONFIG_PATH=/etc/containerd/certs.d FAKE_CRI_PATH=/opt/unrelated-certs FAKE_MIRROR_REQUESTS=1 \
	bash "$subject" "$offline_url" >"$fixture/bad-cri-log" 2>&1; then
	echo 'FAIL: a missing live CRI registry path was accepted' >&2
	exit 1
fi
if PATH="$fixture/bin:$PATH" FAKE_READY_REPLICAS=1 \
	FAKE_CONFIG_PATH=/etc/containerd/certs.d FAKE_CRI_PATH=/etc/containerd/certs.d FAKE_MIRROR_REQUESTS=0 \
	bash "$subject" "$offline_url" >"$fixture/no-mirror-log" 2>&1; then
	echo 'FAIL: Redis readiness without a mirror request was accepted' >&2
	exit 1
fi

echo 'PASS: offline ECR consumer gate requires CRI routing, a mirror request, and ready Redis'
