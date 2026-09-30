#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
validator="${script_dir}/verify-desktop-codeql.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT

cat >"${scratch}/positive.json" <<'JSON'
{"#select":{"tuples":[
["main.go","main",1],
["desktop/main.go","main",1],
["desktop/main.go","run",1],
["desktop/env_other.go","hydrateLoginShellEnv",1],
["desktop/menu.go","installApplicationMenu",1],
["desktop/deeplink.go","handleDeepLink",1],
["desktop/notify.go","watchClusterStatus",1],
["desktop/window_state.go","trackWindowState",1]
]}}
JSON

bash "${validator}" --results "${scratch}/positive.json" >/dev/null

reject() {
	local name="$1"
	if bash "${validator}" --results "${scratch}/${name}.json" >/dev/null 2>&1; then
		printf 'FAIL: accepted %s extraction evidence\n' "${name}" >&2
		exit 1
	fi
}

for index in {0..7}; do
	jq --argjson i "${index}" '."#select".tuples[$i][2] = 0' "${scratch}/positive.json" >"${scratch}/zero.json"
	reject zero
	jq --argjson i "${index}" 'del(."#select".tuples[$i])' "${scratch}/positive.json" >"${scratch}/missing.json"
	reject missing
done

jq '."#select".tuples[1] = ."#select".tuples[0]' "${scratch}/positive.json" >"${scratch}/duplicate.json"
reject duplicate
jq '."#select".tuples[3][0] = "desktop/env_darwin.go"' "${scratch}/positive.json" >"${scratch}/wrong-platform.json"
reject wrong-platform
jq '."#select".tuples[0][2] = "1"' "${scratch}/positive.json" >"${scratch}/string-count.json"
reject string-count
printf '{}\n' >"${scratch}/empty.json"
reject empty
printf '{invalid\n' >"${scratch}/malformed.json"
reject malformed

printf 'All desktop CodeQL coverage cases passed.\n'
