#!/usr/bin/env bash
# Classify a full CD attempt before building, or independently recheck its replay proof.
# Existing immutable assets and OCI publications are never rebuilt or overwritten here.
set -euo pipefail
export LC_ALL=C

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
bash "$root_dir/.github/scripts/validate-release-ref.sh"
repo=devantler-tech/ksail
tag=${GITHUB_REF#refs/tags/}
version=${tag#v}
sha=${GITHUB_SHA-}
run=${GITHUB_RUN_ID-}
attempt=${GITHUB_RUN_ATTEMPT-}

blocked() {
  printf 'BLOCKED: %s\n' "$1" >&2
  exit 1
}
[[ ${GITHUB_REPOSITORY-} == "$repo" && ${GITHUB_EVENT_NAME-} == push &&
  $sha =~ ^[0-9a-f]{40}$ && $run =~ ^[1-9][0-9]*$ && $attempt =~ ^[1-9][0-9]*$ ]] ||
  blocked 'invalid release replay context'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

emit() {
  printf '%s\n' "$1"
  if [[ -n ${GITHUB_OUTPUT-} ]]; then printf '%s\n' "$1" >>"$GITHUB_OUTPUT"; fi
}

# A tag-specific GraphQL lookup avoids enumerating the entire release history on every tag.
query=$(
  cat <<'GRAPHQL'
query($tag:String!){repository(owner:"devantler-tech",name:"ksail"){nameWithOwner release(tagName:$tag){databaseId tagName isDraft}}}
GRAPHQL
)
if ! gh api graphql -f "query=$query" -f "tag=$tag" >"$tmp/state.json"; then
  blocked 'could not read release state'
fi
jq -e --arg repo "$repo" '.errors == null and .data.repository.nameWithOwner == $repo and
  (.data.repository | has("release"))' "$tmp/state.json" >/dev/null || blocked 'unreadable release state'

# The immutable tag and the original CD run must identify the same source. Follow annotated
# tags only through bounded, same-repository tag objects; no response supplies a URL to execute.
if ! gh api "repos/$repo/git/ref/tags/$tag" >"$tmp/ref.json"; then blocked 'could not read source tag'; fi
jq -e --arg ref "$GITHUB_REF" '.ref == $ref' "$tmp/ref.json" >/dev/null || blocked 'source tag differs'
for _ in 1 2 3 4; do
  kind=$(jq -er '.object.type' "$tmp/ref.json") || blocked 'unreadable source tag object'
  object=$(jq -er '.object.sha | select(test("^[0-9a-f]{40}$"))' "$tmp/ref.json") || blocked 'unreadable source tag digest'
  if [[ $kind == commit ]]; then break; fi
  [[ $kind == tag ]] || blocked 'source tag does not point to a commit'
  if ! gh api "repos/$repo/git/tags/$object" >"$tmp/ref-next.json"; then blocked 'could not read annotated source tag'; fi
  mv "$tmp/ref-next.json" "$tmp/ref.json"
done
[[ $kind == commit && $object == "$sha" ]] || blocked 'source/tag binding differs'

# A tag lookup returns one release. An existing publication also needs a complete
# conflict census so an additional draft cannot be hidden by that lookup.
if jq -e '.data.repository.release != null' "$tmp/state.json" >/dev/null; then
  if ! gh api --paginate "repos/$repo/releases?per_page=100" >"$tmp/release-pages.json"; then
    blocked 'could not read conflicting release records'
  fi
  jq -se --arg tag "$tag" --slurpfile state "$tmp/state.json" '
    [.[] | if type == "array" then .[] else error("invalid release page") end |
      select(.tag_name == $tag)] |
    length == 1 and .[0].id == $state[0].data.repository.release.databaseId
  ' "$tmp/release-pages.json" >/dev/null || blocked 'conflicting release records for the tag'
fi

if jq -e '.data.repository.release == null or .data.repository.release.isDraft == true' "$tmp/state.json" >/dev/null; then
  [[ -z ${REPLAY_EXPECTED-} ]] || blocked 'expected immutable publication is missing'
  emit mode=build
  emit "attempt=$attempt"
  exit 0
fi
[[ $attempt -gt 1 ]] || blocked 'an existing published release requires its original full-run replay'
release_id=$(jq -er --arg tag "$tag" '.data.repository.release |
  select(.tagName == $tag and .isDraft == false) | .databaseId |
  select(type == "number" and . > 0 and floor == .)' "$tmp/state.json") || blocked 'unreadable release identity'

if ! gh api "repos/$repo/releases/$release_id" >"$tmp/release.json"; then blocked 'could not read immutable release'; fi
jq -e --arg tag "$tag" --argjson id "$release_id" '.id == $id and .tag_name == $tag and
  .draft == false and .immutable == true' "$tmp/release.json" >/dev/null || blocked 'release is not the expected immutable publication'
if ! gh api "repos/$repo/actions/runs/$run/attempts/1" >"$tmp/run.json"; then blocked 'could not read original publisher run'; fi
jq -e --argjson run "$run" --arg sha "$sha" --arg tag "$tag" --arg repo "$repo" '
  .id == $run and .run_attempt == 1 and .head_sha == $sha and .head_branch == $tag and
  .event == "push" and .path == ".github/workflows/cd.yaml" and .repository.full_name == $repo and
  (.actor.login == "devantler" or .actor.login == "ksail-bot[bot]" or .actor.login == "github-actions[bot]")
' "$tmp/run.json" >/dev/null || blocked 'original publisher run/source differs'

if ! gh api --paginate "repos/$repo/actions/runs/$run/attempts/1/jobs?per_page=100" >"$tmp/job-pages.json"; then
  blocked 'could not read original publisher jobs'
fi
jq -se '[.[] | .jobs[]] as $jobs | [
  "🧩 Release CLI", "🧩 Release VSCode Extension", "🧩 Release Desktop App (macOS cask)",
  "🧩 Release Desktop App (ubuntu-24.04, linux, amd64)",
  "🧩 Release Desktop App (windows-latest, windows, amd64, .exe)",
  "⛵ Release Operator Chart", "📄 Build Pages", "📢 Publish Release"
] | all(. as $name | [$jobs[] | select(.name == $name)] |
  length == 1 and .[0].status == "completed" and .[0].conclusion == "success")
' "$tmp/job-pages.json" >/dev/null || blocked 'original publisher jobs are incomplete or failed'

if ! gh api --paginate "repos/$repo/releases/$release_id/assets?per_page=100" >"$tmp/asset-pages.json"; then
  blocked 'could not read release asset set'
fi
jq -se 'add | select(type == "array") | select(all(
  (.id | type == "number" and . > 0 and floor == .) and (.size | type == "number" and . > 0) and
  (.digest | type == "string" and test("^sha256:[0-9a-f]{64}$")))) |
  select((map(.name) | unique | length) == length) |
  select((map(.id) | unique | length) == length) |
  sort_by(.name) | map({id,name,size,digest})' "$tmp/asset-pages.json" >"$tmp/assets.json" || blocked 'invalid release asset set'

expected=(install.sh "ksail_${version}_checksums.txt" "ksail_${version}.vsix"
  "KSail_${version}_darwin_arm64.zip" "ksail-desktop_${version}_linux_amd64.tar.gz"
  "ksail-desktop_${version}_windows_amd64.zip")
archives=("ksail_${version}_darwin_arm64.tar.gz" "ksail_${version}_linux_amd64.tar.gz"
  "ksail_${version}_linux_arm64.tar.gz" "ksail_${version}_windows_amd64.zip" "ksail_${version}_windows_arm64.zip")
expected+=("${archives[@]}")
shopt -s nullglob
crds=("$root_dir"/charts/ksail-operator/crds/*.yaml)
[[ ${#crds[@]} -gt 0 ]] || blocked 'release source contains no CRD assets'
for file in "${crds[@]}"; do expected+=("$(basename "$file")"); done
printf '%s\n' "${expected[@]}" | jq -Rs 'split("\n")[:-1] | sort' >"$tmp/expected.json"
jq -e --slurpfile expected "$tmp/expected.json" 'map(.name) == $expected[0]' "$tmp/assets.json" >/dev/null || blocked 'release asset set differs from all intended targets'

# Use the CLI's VERIFIED statement, not an unverified bundle payload. It binds the tag, commit
# and every asset digest to GitHub's release attestation, independently of mutable release notes.
if ! gh release verify "$tag" --repo "$repo" --format json >"$tmp/attestation.json"; then blocked 'release attestation verification failed'; fi
jq -e --arg repo "$repo" --arg tag "$tag" --arg sha "$sha" --arg id "$release_id" --slurpfile assets "$tmp/assets.json" '
  .verificationResult.statement as $s | $s._type == "https://in-toto.io/Statement/v1" and
  $s.predicateType == "https://in-toto.io/attestation/release/v0.2" and
  $s.predicate.repository == $repo and $s.predicate.repositoryId == "737584922" and
  $s.predicate.tag == $tag and $s.predicate.databaseId == $id and
  ([$s.subject[] | select(.uri == ("pkg:github/" + $repo + "@" + $tag) and .digest.sha1 == $sha)] | length) == 1 and
  ($s.subject | length) == (($assets[0] | length) + 1) and
  ([$s.subject[] | select(has("name")) | {name,digest:("sha256:" + .digest.sha256)}] | sort_by(.name)) ==
  ($assets[0] | map({name,digest}))
' "$tmp/attestation.json" >/dev/null || blocked 'release attestation source or asset digests differ'

for file in "$root_dir/install.sh" "${crds[@]}"; do
  name=$(basename "$file")
  digest=$(sha256sum -- "$file" | cut -d ' ' -f 1)
  jq -e --arg name "$name" --arg digest "sha256:$digest" 'any(.name == $name and .digest == $digest)' "$tmp/assets.json" >/dev/null || blocked 'release extra asset differs from its source'
done
checksums_name="ksail_${version}_checksums.txt"
checksums_id=$(jq -er --arg name "$checksums_name" '.[] | select(.name == $name) | .id' "$tmp/assets.json")
if ! gh api "repos/$repo/releases/assets/$checksums_id" -H 'Accept: application/octet-stream' >"$tmp/checksums.txt"; then blocked 'could not read attested archive checksums'; fi
digest=$(sha256sum -- "$tmp/checksums.txt" | cut -d ' ' -f 1)
jq -e --arg name "$checksums_name" --arg digest "sha256:$digest" 'any(.name == $name and .digest == $digest)' "$tmp/assets.json" >/dev/null || blocked 'downloaded checksum manifest differs from attestation'
listed=()
while IFS= read -r line; do
  [[ $line =~ ^([0-9a-fA-F]{64})[[:space:]]+\*?([^[:space:]/]+)$ ]] || blocked 'malformed archive checksum manifest'
  checksum=${BASH_REMATCH[1]}
  name=${BASH_REMATCH[2]}
  listed+=("$name")
  jq -e --arg name "$name" --arg digest "sha256:$checksum" 'any(.name == $name and .digest == ($digest | ascii_downcase))' "$tmp/assets.json" >/dev/null || blocked 'archive checksum differs from attested asset'
done <"$tmp/checksums.txt"
printf '%s\n' "${listed[@]}" | sort >"$tmp/listed"
printf '%s\n' "${archives[@]}" | sort >"$tmp/archives"
cmp -s "$tmp/listed" "$tmp/archives" || blocked 'archive checksum set does not cover every CLI target exactly once'

# An unchanged GitHub release alone proves nothing about OCI tags. Verify each existing
# publication with its original tag workflow AND source commit; record the actual signed digest.
identity="https://github.com/$repo/.github/workflows/cd.yaml@$GITHUB_REF"
printf '{}\n' >"$tmp/images.json"
for image in "ghcr.io/$repo:$tag" "ghcr.io/devantler-tech/ksail-steer:$tag" "ghcr.io/devantler-tech/charts/ksail-operator:$version"; do
  if ! cosign verify --certificate-identity "$identity" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-github-workflow-sha "$sha" "$image" >"$tmp/signature.json"; then
    blocked 'OCI publication is missing, unsigned or bound to a different publisher/source'
  fi
  digest=$(jq -er '[.[].critical.image."docker-manifest-digest"] | unique |
    select(length == 1) | .[0] | select(test("^sha256:[0-9a-f]{64}$"))' "$tmp/signature.json") || blocked 'OCI signature has no unambiguous manifest digest'
  jq --arg image "$image" --arg digest "$digest" '. + {($image):$digest}' "$tmp/images.json" >"$tmp/images-next.json"
  mv "$tmp/images-next.json" "$tmp/images.json"
done
proof=$(jq -cnS --arg tag "$tag" --arg sha "$sha" --argjson run "$run" --argjson id "$release_id" \
  --slurpfile assets "$tmp/assets.json" --slurpfile images "$tmp/images.json" \
  '{tag:$tag,source:$sha,originalRun:$run,releaseId:$id,assets:$assets[0],images:$images[0]}')
if [[ -n ${REPLAY_EXPECTED-} ]]; then
  expected_proof=$(jq -ceS . <<<"$REPLAY_EXPECTED") || blocked 'unreadable original replay proof'
  [[ $proof == "$expected_proof" ]] || blocked 'published GitHub or OCI content changed during replay'
fi
emit mode=verify
emit "attempt=$attempt"
emit "proof=$proof"
