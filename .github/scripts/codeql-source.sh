#!/usr/bin/env bash
# Bind resource measurements to one immutable source revision.
source_matches_head() {
	local head untracked
	head="$(git rev-parse HEAD)" && [[ "$head" == "$1" ]] &&
		git diff --quiet "$1" -- '*.go' '*go.mod' '*go.sum' '.github/scripts' '.github/codeql' &&
		untracked="$(git ls-files --others -- '*.go' '*go.mod' '*go.sum')" &&
		[[ -z "$untracked" ]]
}
