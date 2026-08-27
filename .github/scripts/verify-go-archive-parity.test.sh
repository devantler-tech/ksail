#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
validator="${script_dir}/verify-go-archive-parity.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

upstream="${tmp_dir}/upstream"
local_copy="${tmp_dir}/local"
mkdir -p "${upstream}/.github" "${local_copy}"
printf 'package archive\n' >"${upstream}/archive.go"
printf 'ignored repository metadata\n' >"${upstream}/.github/workflow.yml"
cp "${upstream}/archive.go" "${local_copy}/archive.go"
printf 'package archive\n' >"${local_copy}/compat_legacy.go"
printf 'package archive\n' >"${local_copy}/compat_legacy_test.go"
printf '# Local patch\n' >"${local_copy}/KSail-PATCH.md"

"${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}"

printf 'package changed\n' >"${local_copy}/archive.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: modified upstream source passed parity validation\n' >&2
	exit 1
fi

cp "${upstream}/archive.go" "${local_copy}/archive.go"
printf 'package unexpected\n' >"${local_copy}/unexpected.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: unexpected local source passed parity validation\n' >&2
	exit 1
fi

rm "${local_copy}/unexpected.go"
printf 'module runtime data\n' >"${upstream}/.runtime"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: missing hidden module file passed parity validation\n' >&2
	exit 1
fi

rm "${upstream}/.runtime"

# `diff --exclude=PATTERN` matched a BASENAME at every recursion level, so an
# exception granted to a top-level entry silently covered nested files and
# directories of the same name — a path for undeclared code to ride into the
# vendored copy. Each case below must be REJECTED, and the positive control
# after them proves the anchoring, not a permanently-failing validator.
mkdir -p "${local_copy}/pkg"
printf 'package smuggled\n' >"${local_copy}/pkg/compat_legacy.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: nested excepted-basename file passed parity validation\n' >&2
	exit 1
fi
rm -r "${local_copy}/pkg"

mkdir -p "${local_copy}/pkg/.github"
printf 'nested metadata\n' >"${local_copy}/pkg/.github/workflow.yml"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: nested excepted-basename directory passed parity validation\n' >&2
	exit 1
fi
rm -r "${local_copy}/pkg"

mkdir -p "${upstream}/pkg"
printf 'package upstreamonly\n' >"${upstream}/pkg/compat_legacy.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: nested excepted-basename file missing locally passed parity validation\n' >&2
	exit 1
fi
rm -r "${upstream}/pkg"

# `diff -u` FOLLOWS symbolic links, so it exits 0 comparing an upstream regular
# file against a local symlink whose target holds identical bytes. Enumeration
# admits symlinks (`-type l`), so a replacement such as
# `archive.go -> ../unreviewed/archive.go` passed parity while RESOLVING OUTSIDE
# the parity-checked module — after which its target can change without this
# check ever seeing it. File TYPE must match, not just content.
mkdir -p "${tmp_dir}/unreviewed"
cp "${upstream}/archive.go" "${tmp_dir}/unreviewed/archive.go"
rm "${local_copy}/archive.go"
ln -s '../unreviewed/archive.go' "${local_copy}/archive.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: local symlink replacing an upstream regular file passed parity validation\n' >&2
	exit 1
fi
rm "${local_copy}/archive.go"
cp "${upstream}/archive.go" "${local_copy}/archive.go"

# A symlink UPSTREAM against a local regular file is the mirror case: it must be
# rejected too, so the guard cannot be satisfied by swapping which side is typed.
ln -s '../unreviewed/archive.go' "${upstream}/link_only.go"
printf 'package archive\n' >"${local_copy}/link_only.go"
if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: upstream symlink against a local regular file passed parity validation\n' >&2
	exit 1
fi
rm "${upstream}/link_only.go" "${local_copy}/link_only.go"

# The two COMPATIBILITY Go files are parity exceptions, so they are filtered out
# of the comparable-file lists before the shared-file symlink guard ever runs —
# that guard therefore never examines them. They are still compiled as part of
# the module, so a link at either path lets the Go tool build source from outside
# the provenance-checked tree, with its target repointable afterwards. Each is
# asserted separately: one guard covering only the other path would pass here.
for compat in compat_legacy.go compat_legacy_test.go; do
	mkdir -p "${tmp_dir}/unreviewed"
	printf 'package archive\n' >"${tmp_dir}/unreviewed/${compat}"
	rm -f "${local_copy}/${compat}"
	ln -s "../unreviewed/${compat}" "${local_copy}/${compat}"
	if "${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
		printf 'FAIL: symbolic link at compatibility path %s passed parity validation\n' "${compat}" >&2
		exit 1
	fi
	rm -f "${local_copy}/${compat}"
	printf 'package archive\n' >"${local_copy}/${compat}"
done

# A newline inside a module path would let one entry be read as two by any
# consumer that splits on newlines, so enumeration rejects it outright. The
# name spliced here is two EXCEPTED basenames joined by a newline: the guard
# has to fire on the path itself, before the exception filter would otherwise
# drop both halves and hide it.
#
# The assertion is on the guard's OWN message, not merely on a non-zero exit:
# with the guard removed this path is still rejected, because the embedded
# newline splits the name into two phantom entries that fail the file-list
# comparison instead. Exit status alone therefore passes with the guard gone
# and pins nothing (measured).
newline_path="${local_copy}/$(printf 'KSail-PATCH.md\ncompat_legacy.go')"
printf 'package archive\n' >"${newline_path}"
newline_output="$("${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" 2>&1)" \
	&& newline_rc=0 || newline_rc=$?
if ((newline_rc == 0)); then
	printf 'FAIL: newline in a module path passed parity validation\n' >&2
	exit 1
fi
if ! grep -q 'newline in module path is not permitted' <<<"${newline_output}"; then
	printf 'FAIL: newline path was rejected, but not by the newline guard\n' >&2
	printf '%s\n' "${newline_output}" >&2
	exit 1
fi
rm -f -- "${newline_path}"

# The reviewed pin is asserted against EVERY manifest that requires the module:
# a manifest left on a superseded version would have Go advertise that version's
# metadata while these bytes are the reviewed ones, so version-based
# vulnerability analysis would credit the binary with fixes never compiled.
#
# Exercised against a THROWAWAY repository tree rather than this checkout's real
# go.mod files: repo_root is derived from the validator's own location, so a
# relocated copy reads the manifests beside it and no tracked file is ever
# mutated (a test that edits real manifests leaves them corrupted if it aborts).
fake_repo="${tmp_dir}/fake-repo"
mkdir -p "${fake_repo}/.github/scripts" "${fake_repo}/desktop"
cp "${validator}" "${fake_repo}/.github/scripts/"
fake_validator="${fake_repo}/.github/scripts/${validator##*/}"

write_fake_manifests() {
	printf 'module fake\n\nrequire (\n\tgithub.com/moby/go-archive %s // indirect\n)\n' "$1" \
		>"${fake_repo}/go.mod"
	printf 'module fake/desktop\n\nrequire (\n\tgithub.com/moby/go-archive %s // indirect\n)\n' "$2" \
		>"${fake_repo}/desktop/go.mod"
}

# POSITIVE CONTROL first: with both manifests on the reviewed pin the relocated
# validator passes, so the two rejections below are attributable to the version
# and not to the fake tree merely being unusable.
write_fake_manifests 'v0.3.0' 'v0.3.0'
if ! "${fake_validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: relocated validator rejected manifests that are on the reviewed pin\n' >&2
	exit 1
fi

# Each manifest is asserted separately: a check covering only the other one
# would still pass here.
write_fake_manifests 'v0.2.0' 'v0.3.0'
if "${fake_validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: go.mod requiring a superseded version passed parity validation\n' >&2
	exit 1
fi

write_fake_manifests 'v0.3.0' 'v0.2.0'
if "${fake_validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}" >/dev/null 2>&1; then
	printf 'FAIL: desktop/go.mod requiring a superseded version passed parity validation\n' >&2
	exit 1
fi

"${validator}" --upstream-dir "${upstream}" --local-dir "${local_copy}"

printf 'All go-archive parity cases passed.\n'
