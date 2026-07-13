#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  collect-cask-pr-handoff.sh --tap OWNER/REPO --pr NUMBER --output FILE

Collect a stable normalized snapshot of a generated Homebrew cask pull request:
REST PR identity, every paginated changed file, the repository label inventory,
and — when exactly one file changed — that file's content at the PR head, so the
validator can prove the cask actually pins the release being handed off.
Any API or response-shape failure exits non-zero without publishing partial output.
EOF
}

tap=""
pr=""
output=""

while (($# > 0)); do
	case "$1" in
	--tap)
		tap="${2:-}"
		shift 2
		;;
	--pr)
		pr="${2:-}"
		shift 2
		;;
	--output)
		output="${2:-}"
		shift 2
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		printf 'ERROR: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "${tap}" =~ ^[^/]+/[^/]+$ || ! "${pr}" =~ ^[1-9][0-9]*$ || -z "${output}" ]]; then
	printf 'ERROR: --tap OWNER/REPO, --pr NUMBER, and --output FILE are required\n' >&2
	usage >&2
	exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

gh api "repos/${tap}/pulls/${pr}" >"${work}/pr.json"
gh api --paginate --slurp \
	"repos/${tap}/pulls/${pr}/files?per_page=100" >"${work}/file-pages.json"
gh api --paginate --slurp \
	"repos/${tap}/labels?per_page=100" >"${work}/label-pages.json"

jq -e 'type == "object"' "${work}/pr.json" >/dev/null
jq -e 'type == "array" and all(.[]; type == "array")' \
	"${work}/file-pages.json" >/dev/null
jq -e '
  type == "array"
  and all(.[]; type == "array")
  and all(.[][]; (.name | type == "string" and length > 0))
' \
	"${work}/label-pages.json" >/dev/null

# When the PR changes exactly one file (the only shape the validator accepts),
# capture that file's content at the PR head so the validator can check the cask
# pins the current release. Any other shape records null — the validator blocks
# on file scope and on missing content evidence alike (fail closed).
printf 'null\n' >"${work}/head-file.json"
head_sha="$(jq -r '.head.sha // empty' "${work}/pr.json")"
single_file="$(jq -r 'if ([.[][]] | length) == 1 then [.[][]][0].filename else "" end' \
	"${work}/file-pages.json")"
if [[ -n "${head_sha}" && -n "${single_file}" ]]; then
	gh api "repos/${tap}/contents/${single_file}?ref=${head_sha}" >"${work}/head-file.json"
	jq -e '
    type == "object"
    and (.path | type == "string" and length > 0)
    and (.content | type == "string")
  ' "${work}/head-file.json" >/dev/null
fi

jq -n \
	--slurpfile pr "${work}/pr.json" \
	--slurpfile pages "${work}/file-pages.json" \
	--slurpfile label_pages "${work}/label-pages.json" \
	--slurpfile head_file "${work}/head-file.json" '
    {
      pr: $pr[0],
      files: [$pages[0][][]],
      availableLabels: [$label_pages[0][][]],
      headFile: $head_file[0]
    }
  ' >"${work}/evidence.json"

mv "${work}/evidence.json" "${output}"
