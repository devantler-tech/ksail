#!/usr/bin/env bash
# A tag link in an archived ECR mirror does not prove the consumer pull works.
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/mirror-cache" "$fixture/contents/docker/registry/v2/repositories/docker/library/redis/_manifests/tags/8.6.4-alpine/current" "$fixture/bin"
printf 'sha256:present-tag-only\n' > "$fixture/contents/docker/registry/v2/repositories/docker/library/redis/_manifests/tags/8.6.4-alpine/current/link"
tar -cf "$fixture/mirror-cache/ecr-public.aws.com.tar" -C "$fixture/contents" .
for registry in docker.io ghcr.io quay.io registry.k8s.io; do
  : > "$fixture/mirror-cache/$registry.tar"
done
printf 'ecr-public.aws.com/docker/library/redis:8.6.4-alpine\n' > "$fixture/all-images.txt"

cat > "$fixture/bin/docker" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_CALLS"
if [ "$1" = pull ]; then
  exit 1
fi
exit 0
SH
cat > "$fixture/bin/curl" <<'SH'
#!/bin/sh
exit 0
SH
cat > "$fixture/bin/sleep" <<'SH'
#!/bin/sh
exit 0
SH
chmod +x "$fixture/bin/docker" "$fixture/bin/curl" "$fixture/bin/sleep"

# Execute the action's real check step with only its absolute runner paths
# redirected into this disposable fixture.
awk '
  /^    - name: 🔍 Check if cache is complete$/ { step = 1; next }
  step && /^    - name:/ { exit }
  step && /^      run: \|$/ { body = 1; next }
  body { sub(/^        /, ""); print }
' "$action_dir/action.yaml" > "$fixture/raw.sh"
test -s "$fixture/raw.sh"
sed "s#/tmp/mirror-cache#$fixture/mirror-cache#g; s#/tmp/all-images.txt#$fixture/all-images.txt#g" \
  "$fixture/raw.sh" > "$fixture/check.sh"
PATH="$fixture/bin:$PATH" GITHUB_OUTPUT="$fixture/output" DOCKER_CALLS="$fixture/docker-calls" \
  GITHUB_RUN_ID=17 GITHUB_RUN_ATTEMPT=2 CACHE_KEY=mirror-test \
  bash "$fixture/check.sh" > "$fixture/log" 2>&1

if ! grep -qx 'complete=false' "$fixture/output"; then
  echo "FAIL: a tag-only archive was accepted as a complete mirror cache" >&2
  cat "$fixture/log" >&2
  exit 1
fi
if ! grep -q '^pull .*redis:8.6.4-alpine' "$fixture/docker-calls"; then
  echo "FAIL: the restored mirror was not checked through the consumer pull path" >&2
  exit 1
fi
if grep -q 'REGISTRY_PROXY_REMOTEURL' "$fixture/docker-calls"; then
  echo "FAIL: validation allowed a remote registry fallback" >&2
  exit 1
fi
if ! grep -qx 'repair-key=mirror-test-repair-17-2' "$fixture/output"; then
  echo "FAIL: an invalid immutable cache needs a fresh repair key" >&2
  exit 1
fi
echo "PASS: an unusable restored ECR image is rejected after a pull check"
