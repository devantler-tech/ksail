#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Usage:
  collect-cask-pr-handoff.sh --tap OWNER/REPO --pr NUMBER --output FILE

Collect a stable normalized snapshot of a generated Homebrew cask pull request:
REST PR identity, every paginated changed file, and the repository label inventory.
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

jq -n \
	--slurpfile pr "${work}/pr.json" \
	--slurpfile pages "${work}/file-pages.json" \
	--slurpfile label_pages "${work}/label-pages.json" '
    {
      pr: $pr[0],
      files: [$pages[0][][]],
      availableLabels: [$label_pages[0][][]]
    }
  ' >"${work}/evidence.json"

mv "${work}/evidence.json" "${output}"
