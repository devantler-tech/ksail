#!/usr/bin/env bash
set -euo pipefail

subject="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/reject-remote-kustomize-resources.sh"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

run_case() {
	local name=$1 expected=$2 resource=$3 style=${4:-block} fixture result=0
	fixture="$(mktemp -d)"
	trap 'rm -rf "$fixture"' RETURN
	if [ "$style" = flow ]; then
		printf 'resources: [%s]\n' "$resource" >"$fixture/kustomization.yaml"
	else
		printf 'resources:\n  - %s\n' "$resource" >"$fixture/kustomization.yaml"
	fi

	"$subject" "$fixture" >/dev/null 2>&1 || result=$?
	if [ "$expected" = reject ] && [ "$result" -eq 0 ]; then
		fail "$name was accepted"
	fi
	if [ "$expected" = accept ] && [ "$result" -ne 0 ]; then
		fail "$name was rejected"
	fi
	rm -rf "$fixture"
	trap - RETURN
}

run_case local accept ./base
run_case https reject https://github.com/stefanprodan/podinfo//kustomize
run_case ssh reject ssh://git@github.com/stefanprodan/podinfo
run_case scp reject git@github.com:stefanprodan/podinfo
run_case shorthand reject github.com/stefanprodan/podinfo
run_case shorthand-colon reject github.com:stefanprodan/podinfo
run_case legacy-prefix reject git::https://github.com/stefanprodan/podinfo//kustomize
run_case uppercase-legacy-prefix reject GIT::HTTPS://github.com/stefanprodan/podinfo//kustomize
run_case flow-list reject https://github.com/stefanprodan/podinfo//kustomize flow

printf 'remote-resource-guard tests passed\n'
