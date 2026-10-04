#!/usr/bin/env bash
# Bind resource measurements to one immutable source revision.
source_matches_head() {
	[[ "$(git rev-parse HEAD)" == "$1" ]] &&
		git diff --quiet "$1" -- '*.go' '*go.mod' '*go.sum' '.github/scripts' '.github/codeql' &&
		[[ -z "$(git ls-files --others --exclude-standard -- '*.go' '*go.mod' '*go.sum')" ]]
}
