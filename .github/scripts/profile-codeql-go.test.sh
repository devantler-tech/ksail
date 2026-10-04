#!/usr/bin/env bash
# Exercise the actual extraction loop and native time, with only the extractor
# replaced by a fixture command. No CodeQL database or network is needed.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
source "$script_dir/codeql-source.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/source/third_party/fails" "$scratch/source/third_party/passes" "$scratch/metrics"
touch "$scratch/source/go.mod" "$scratch/source/third_party/fails/go.mod" "$scratch/source/third_party/passes/go.mod"
printf '%s\n' root third_party/fails third_party/passes root-desktop >"$scratch/inventory"
source_root="$(cd "$script_dir/../.." && pwd)"
(cd "$source_root" && go build -o "$scratch/report" ./internal/codeqlprofile/cmd)
printf '{"sourceVerified":false,"coverageVerified":false}\n' >"$scratch/context.json"
export KSAIL_CODEQL_REPORTER="$scratch/report" KSAIL_CODEQL_CONTEXT="$scratch/context.json" \
	KSAIL_CODEQL_REPORT_PATH="$scratch/progress.json"
cat >"$scratch/extractor" <<'EXTRACTOR'
#!/usr/bin/env bash
set -eu
[[ "$#" == 2 && "$1" == -mod=mod && "$2" == ./... ]]
[[ "$GOWORK" == off ]]
printf '%s\t%s\n' "${PWD##*/}" "${GOFLAGS:-}" >>"$PROFILE_TEST_OBSERVED"
case "$PWD" in
  */fails) exit 23 ;;
  */passes)
    # The previous command's failure must already be durable before this starts.
    jq -e '.complete == false and (.records | length == 2) and .records[1].exitCode == 23' \
      "$KSAIL_CODEQL_REPORT_PATH" >/dev/null ;;
esac
EXTRACTOR
chmod +x "$scratch/extractor"
export PROFILE_TEST_OBSERVED="$scratch/observed"
result=0
(cd "$scratch/source" && bash "$script_dir/profile-codeql-go.sh" --extract \
	"$scratch/inventory" "$scratch/metrics" "$scratch/extractor") || result=$?
[[ "$result" == 23 ]] || {
	printf 'FAIL: lost extractor status %s\n' "$result"
	exit 1
}
[[ "$(wc -l <"$scratch/observed" | tr -d ' ')" == 4 ]]
[[ "$(cat "$scratch/metrics/00000.exit")" == 0 ]]
[[ "$(cat "$scratch/metrics/00001.exit")" == 23 ]]
[[ "$(cat "$scratch/metrics/00002.exit")" == 0 ]]
[[ "$(cat "$scratch/metrics/00003.exit")" == 0 ]]
[[ "$(tail -1 "$scratch/observed")" == $'source\t-tags=desktop' ]]
for index in 00000 00001 00002 00003; do
	[[ -s "$scratch/metrics/$index.time" ]]
done

# Exercise the real JSON command against the native timer output, including a
# failed process. Its output must survive even though its status is unsuccessful.
if "$scratch/report" "$scratch/metrics" <"$scratch/inventory" >"$scratch/report.json"; then
	printf 'FAIL: resource report accepted a failed extraction\n' >&2
	exit 1
fi
jq -e '.complete == false and (.records | length == 4) and
  .records[1].exitCode == 23 and all(.records[]; .stats.commandPeakRssBytes > 0)' \
	"$scratch/report.json" >/dev/null
jq -e '.sourceVerified == false and .coverageVerified == false and (.records | length == 4)' \
	"$scratch/progress.json" >/dev/null

# Inventory text cannot select a directory outside the repository.
printf '%s\n' '../escape' >"$scratch/invalid"
if (cd "$scratch/source" && bash "$script_dir/profile-codeql-go.sh" --extract \
	"$scratch/invalid" "$scratch/metrics" "$scratch/extractor") >/dev/null 2>&1; then
	printf 'FAIL: accepted an escaping project path\n' >&2
	exit 1
fi
printf 'PASS: measured all four commands, retained failure, and rejected escaping paths\n'

# A clean successor commit must not certify measurements from its predecessor.
(
	cd "$scratch/source"
	git init -q
	git -c user.name=Fixture -c user.email=fixture@example.invalid add go.mod third_party/fails/go.mod third_party/passes/go.mod
	git -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm fixture
	measured_sha="$(git rev-parse HEAD)"
	source_matches_head "$measured_sha"
	printf 'module changed\n' >go.mod
	if source_matches_head "$measured_sha"; then exit 1; fi
	git add go.mod
	git -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm successor
	if source_matches_head "$measured_sha"; then
		printf 'FAIL: a clean successor certified the measured revision\n' >&2
		exit 1
	fi
	successor_sha="$(git rev-parse HEAD)"
	source_matches_head "$successor_sha"
	touch untracked.go
	if source_matches_head "$successor_sha"; then exit 1; fi
)
printf 'PASS: source verification rejects dirty, moved, and untracked source\n'
