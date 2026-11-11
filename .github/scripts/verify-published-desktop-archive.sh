#!/usr/bin/env bash
# Verify the immutable Desktop download before delivering or recovering its cask.
set -euo pipefail

if [[ $# != 1 || ! "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
	printf 'Usage: verify-published-desktop-archive.sh TAG\n' >&2
	exit 2
fi

tag="$1"
asset="KSail_${tag#v}_darwin_arm64.zip"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export GH_TOKEN="${KSAIL_CASK_READ_TOKEN:-${GH_TOKEN:-}}"

"${script_dir}/cask-read-api.sh" "repos/devantler-tech/ksail/releases/tags/${tag}" >"${work}/release.json"
if ! digest="$(jq -er --arg tag "$tag" --arg asset "$asset" '
  select(.tag_name == $tag and .draft == false and .immutable == true)
  | select(.published_at | type == "string" and length > 0)
  | [.assets[] | select(.name == $asset)]
  | select(length == 1) | .[0]
  | select(.size | type == "number" and . > 0 and floor == .)
  | .digest | select(type == "string" and test("^sha256:[0-9a-f]{64}$"))
' "${work}/release.json")"; then
	printf 'BLOCKED: immutable published Desktop asset evidence is incomplete\n' >&2
	exit 1
fi

# The destination is fixed by the product and validated tag, never by response text.
archive="${work}/desktop.zip"
curl --fail --location --silent --show-error --max-time 180 \
	--proto '=https' --proto-redir '=https' \
	--output "$archive" \
	"https://github.com/devantler-tech/ksail/releases/download/${tag}/${asset}"
actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
if [[ "sha256:${actual}" != "$digest" ]]; then
	printf 'BLOCKED: downloaded Desktop archive does not match its published digest\n' >&2
	exit 1
fi

bash "${repo_root}/desktop/scripts/verify-macos-archive.sh" "$archive"
printf 'PASS: published Desktop archive %s has a valid resource seal (%s)\n' "$tag" "$actual"
