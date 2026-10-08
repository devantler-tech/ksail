#!/usr/bin/env bash
set -euo pipefail

action_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
subject="$action_dir/calico-migration.sh"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/baseline"
printf '#!/usr/bin/env bash\nexit 0\n' >"$fixture/baseline/ksail"
chmod +x "$fixture/baseline/ksail"
checksum=$(shasum -a 256 "$fixture/baseline/ksail" | cut -d ' ' -f 1)

prepare() {
  BASELINE_BINARY="$fixture/baseline/ksail" BASELINE_SHA256="$1" \
    DISTRIBUTION="$2" PROVIDER=Docker INIT=true RUNNER_TEMP="$fixture" \
    GITHUB_OUTPUT="$fixture/output" \
    bash "$subject" prepare
}

[[ -f "$subject" ]] || {
  echo 'FAIL: Calico migration helper is missing' >&2
  exit 1
}
prepare "$checksum" Vanilla
grep -Fxq "path=$fixture/baseline:$PATH" "$fixture/output"
for distribution in K3s VCluster; do prepare "$checksum" "$distribution"; done

for invalid in checksum unsupported empty symlink; do
  rm -f "$fixture/output"
  case "$invalid" in
  checksum)
    candidate_checksum=$(printf '%064d' 0)
    distribution=Vanilla
    ;;
  unsupported)
    candidate_checksum="$checksum"
    distribution=Talos
    ;;
  empty)
    candidate_checksum="$checksum"
    distribution=Vanilla
    cp "$fixture/baseline/ksail" "$fixture/saved"
    : >"$fixture/baseline/ksail"
    ;;
  symlink)
    candidate_checksum="$checksum"
    distribution=Vanilla
    mv "$fixture/baseline/ksail" "$fixture/saved"
    ln -s "$fixture/saved" "$fixture/baseline/ksail"
    ;;
  esac
  if prepare "$candidate_checksum" "$distribution" >"$fixture/$invalid.log" 2>&1; then
    echo "FAIL: invalid baseline accepted: $invalid" >&2
    exit 1
  fi
  [[ ! -e "$fixture/output" ]] || {
    echo 'FAIL: invalid baseline selected creation PATH' >&2
    exit 1
  }
  if [[ "$invalid" == empty || "$invalid" == symlink ]]; then
    rm "$fixture/baseline/ksail"
    mv "$fixture/saved" "$fixture/baseline/ksail"
  fi
done
echo 'PASS: Calico baseline identity is checked before selecting the creation binary'

mkdir -p "$fixture/bin" "$fixture/project"
target_version=$(sed -nE 's/^FROM docker.io\/calico\/node:(v[0-9]+\.[0-9]+\.[0-9]+)@sha256:.*/\1/p' "$action_dir/../../../pkg/svc/installer/cni/calico/Dockerfile")
[[ "$target_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
printf 'apiVersion: ksail.io/v1alpha1\nkind: Cluster\nspec:\n  cluster:\n    connection:\n      kubeconfig: %s/calico-migration.kubeconfig\n' "$fixture" >"$fixture/project/ksail.yaml"
printf 'fixture kubeconfig\n' >"$fixture/calico-migration.kubeconfig"
jq -n '{"current-context":"fixture",contexts:[{name:"fixture",context:{cluster:"fixture"}}],clusters:[{name:"fixture"}]}' >"$fixture/target.json"
# Calico 3.32.2 has 31 CRDs and six admission prerequisites. The candidate
# adds two prerequisites; its complete inventory below must still contain 39.
jq -n '{items:[range(1;38) | {kind:"CustomResourceDefinition",metadata:{name:("fixture-" + tostring),uid:("uid-" + tostring),annotations:{"meta.helm.sh/release-name":"calico-crds","meta.helm.sh/release-namespace":"tigera-operator"}}}]}' >"$fixture/identities.json"
jq -n '{serverVersion:{gitVersion:"v1.37.1"}}' >"$fixture/version.json"
jq -n '[{name:"calico",status:"deployed",chart:"tigera-operator-v3.32.2"},{name:"calico-crds",status:"deployed",chart:"projectcalico.org.v3-v3.32.2"}]' >"$fixture/before-releases.json"
jq -n --arg version "$target_version" '[{name:"calico",status:"deployed",chart:("tigera-operator-" + $version)},{name:"calico-crds",status:"deployed",chart:"projectcalico.org.v3-v3.32.2"}]' >"$fixture/after-releases.json"
jq -n '{totalChanges:1,inPlaceChanges:[{field:"cluster.cni.calico.prerequisites"}],rebootRequired:[],recreateRequired:[],rollingRecreate:[],wipeRequired:[],unknownBaseline:[]}' >"$fixture/before-diff.json"
jq -n '{totalChanges:0,inPlaceChanges:[],rebootRequired:[],recreateRequired:[],rollingRecreate:[],wipeRequired:[],unknownBaseline:[]}' >"$fixture/after-diff.json"
jq -n --arg version "$target_version" '{metadata:{labels:{"ksail.io/component":"calico-prerequisites"}},data:{"inventory.json":({complete:true,version:$version,resources:([range(1;40) | {uid:("uid-" + tostring)}] + [{storage:true,resource:"secrets",name:"sh.helm.release.v1.calico-crds.v1",namespace:"tigera-operator",uid:"history-uid"}])} | tojson)}}' >"$fixture/inventory.json"
jq -n --arg version "$target_version" '{spec:{template:{spec:{containers:[{name:"calico-node",image:("docker.io/calico/node:" + $version)}]}}}}' >"$fixture/node.json"

cat >"$fixture/bin/kubectl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ " $* " == *" --kubeconfig $FIXTURE/calico-migration.kubeconfig "* ]]
if [[ "$*" == *'config view'* ]]; then cat "$FIXTURE/target.json"; exit 0; fi
[[ " $* " == *' --context fixture '* ]]
case "$1 $2" in
'get secrets,configmaps')
  if [[ -f "$FIXTURE/updated" && "$CASE" == history-missing ]]; then exit 0; fi
  uid=history-uid
  if [[ -f "$FIXTURE/updated" && "$CASE" == history-replaced ]]; then uid=replacement; fi
  printf 'Secret\tsh.helm.release.v1.calico-crds.v1\t%s\n' "$uid" ;;
'get customresourcedefinitions.'*)
  if [[ "$CASE" == legacy-incomplete ]]; then
    jq '.items |= .[0:-1]' "$FIXTURE/identities.json"
  elif [[ "$CASE" == legacy-extra ]]; then
    jq '.items += [.items[0] | .metadata.name = "extra" | .metadata.uid = "extra"]' "$FIXTURE/identities.json"
  elif [[ -f "$FIXTURE/updated" && "$CASE" == uid ]]; then
    jq '.items[0].metadata.uid = "replacement"' "$FIXTURE/identities.json"
  else cat "$FIXTURE/identities.json"; fi ;;
'get configmap')
  if [[ "$CASE" == history-unrecorded ]]; then
    jq '.data["inventory.json"] |= (fromjson | .resources |= map(select(.storage != true)) | tojson)' "$FIXTURE/inventory.json"
  elif [[ "$CASE" == incomplete ]]; then
    jq '.data["inventory.json"] |= (fromjson | .complete = false | tojson)' "$FIXTURE/inventory.json"
  elif [[ "$CASE" == inventory-short ]]; then
    jq '.data["inventory.json"] |= (fromjson | .resources |= map(select(.uid != "uid-39")) | tojson)' "$FIXTURE/inventory.json"
  else cat "$FIXTURE/inventory.json"; fi ;;
'get daemonset') cat "$FIXTURE/node.json" ;;
'version --request-timeout=30s')
  if [[ "$CASE" == k3s ]]; then jq '.serverVersion.gitVersion = "v1.37.1+k3s1"' "$FIXTURE/version.json"
  else cat "$FIXTURE/version.json"; fi ;;
'rollout status') ;;
*) echo "unexpected kubectl call: $*" >&2; exit 1 ;;
esac
SH
cat >"$fixture/bin/helm" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ " $* " == *" --kubeconfig $FIXTURE/calico-migration.kubeconfig "* && " $* " == *' --kube-context fixture '* ]]
if [[ -f "$FIXTURE/updated" ]]; then cat "$FIXTURE/after-releases.json"
elif [[ "$CASE" == baseline ]]; then printf '[]\n'
else cat "$FIXTURE/before-releases.json"; fi
SH
cat >"$fixture/bin/ksail" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ " $* " == *" --kubeconfig $FIXTURE/calico-migration.kubeconfig "* && " $* " == *' --context fixture '* ]]
case "$1 $2" in
'cluster diff')
  if [[ -f "$FIXTURE/updated" ]]; then
    cat "$FIXTURE/after-diff.json"
    if [[ "$CASE" == diff-status ]]; then exit 2; fi
    exit 0
  fi
  cat "$FIXTURE/before-diff.json"; exit 2 ;;
'cluster update')
  case "$CASE" in
  k3s) [[ " $* " == *' --kubernetes-version v1.37.1-k3s1 '* ]] ;;
  vcluster) [[ " $* " != *' --kubernetes-version '* ]] ;;
  *) [[ " $* " == *' --kubernetes-version v1.37.1 '* ]] ;;
  esac
  if [[ "$CASE" == config ]]; then printf 'changed\n' >>ksail.yaml; fi
  touch "$FIXTURE/updated"
  printf '{}\n' ;;
*) echo "unexpected candidate CLI call: $*" >&2; exit 1 ;;
esac
SH
chmod +x "$fixture/bin/kubectl" "$fixture/bin/helm" "$fixture/bin/ksail"

for scenario in valid k3s vcluster baseline legacy-incomplete legacy-extra incomplete inventory-short uid config history-missing history-replaced history-unrecorded diff-status; do
  rm -f "$fixture/updated"
  printf 'apiVersion: ksail.io/v1alpha1\nkind: Cluster\nspec:\n  cluster:\n    connection:\n      kubeconfig: %s/calico-migration.kubeconfig\n' "$fixture" >"$fixture/project/ksail.yaml"
  status=0
  distribution=Vanilla
  if [[ "$scenario" == k3s ]]; then distribution=K3s; fi
  if [[ "$scenario" == vcluster ]]; then distribution=VCluster; fi
  (cd "$fixture/project" && PATH="$fixture/bin:$PATH" FIXTURE="$fixture" CASE="$scenario" \
    RUNNER_TEMP="$fixture" KUBECONFIG="$fixture/calico-migration.kubeconfig" \
    DISTRIBUTION="$distribution" GITHUB_WORKSPACE="$action_dir/../../.." \
    SYSTEM_TEST_LOG_DIR="$fixture/logs-$scenario" bash "$subject" migrate) \
    >"$fixture/migrate-$scenario.log" 2>&1 || status=$?
  if [[ "$scenario" == valid || "$scenario" == k3s || "$scenario" == vcluster ]]; then
    [[ "$status" == 0 ]] || {
      cat "$fixture/migrate-$scenario.log" >&2
      exit 1
    }
  else
    [[ "$status" != 0 ]] || {
      echo "FAIL: accepted invalid migration: $scenario" >&2
      exit 1
    }
    grep -q 'Calico migration trial:' "$fixture/migrate-$scenario.log" || {
      cat "$fixture/migrate-$scenario.log" >&2
      exit 1
    }
  fi
  if [[ "$scenario" == baseline || "$scenario" == legacy-incomplete || "$scenario" == legacy-extra ]]; then
    [[ ! -f "$fixture/updated" ]] || {
      echo 'FAIL: candidate updated an unverified baseline' >&2
      exit 1
    }
  fi
done
echo 'PASS: migration verifies the legacy release, scoped target, unchanged config, complete inventory and preserved UIDs'
