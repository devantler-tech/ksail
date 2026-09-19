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

run_yaml_case() {
	local name=$1 expected=$2 content=$3 fixture result=0
	fixture="$(mktemp -d)"
	trap 'rm -rf "$fixture"' RETURN
	printf '%s\n' "$content" >"$fixture/kustomization.yaml"

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

run_transitive_case() {
	local fixture result=0
	fixture="$(mktemp -d)"
	trap 'rm -rf "$fixture"' RETURN
	mkdir -p "$fixture/overlays/system-test" "$fixture/base"
	printf 'resources:\n  - ../../base\n' >"$fixture/overlays/system-test/kustomization.yaml"
	printf 'resources:\n  - https://github.com/example/remote//base\n' >"$fixture/base/kustomization.yaml"

	"$subject" "$fixture/overlays/system-test" >/dev/null 2>&1 || result=$?
	if [ "$result" -eq 0 ]; then
		fail "transitive-local-base was accepted"
	fi
	rm -rf "$fixture"
	trap - RETURN
}

run_case local accept ./base
run_case https reject https://github.com/stefanprodan/podinfo//kustomize
run_case ssh reject ssh://git@github.com/stefanprodan/podinfo
run_case scp reject git@github.com:stefanprodan/podinfo
run_case arbitrary-scp-user reject deploy@example.com:org/repo.git//base
run_case shorthand reject github.com/stefanprodan/podinfo
run_case shorthand-colon reject github.com:stefanprodan/podinfo
run_case legacy-prefix reject git::https://github.com/stefanprodan/podinfo//kustomize
run_case uppercase-legacy-prefix reject GIT::HTTPS://github.com/stefanprodan/podinfo//kustomize
run_case flow-list reject https://github.com/stefanprodan/podinfo//kustomize flow
run_yaml_case remote-patch reject $'resources:\n  - ./deployment.yaml\npatches:\n  - path: https://raw.githubusercontent.com/example/repo/main/patch.yaml'
run_yaml_case remote-generator-data reject $'configMapGenerator:\n  - name: data\n    files:\n      - config=https://example.com/config.yaml'
run_transitive_case

printf 'remote-resource-guard tests passed\n'
