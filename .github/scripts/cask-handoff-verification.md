# Verify published cask contents

Run **Verify Published Cask Handoff** from `main` after the reconciliation change
has merged. Supply the published KSail tag and the merged tap PR numbers for
`ksail` and `ksail-desktop`; confirm with `verify published casks`.

The workflow reads both named deliveries through the real merged-handoff finder.
It requires their immutable cask blobs to remain current on the same tap-main
revision and their asset observations to match one stable, published release.
The run logs a compact receipt and retains the full public evidence for seven days.
Movement, incomplete reads, mismatched release assets or an unrelated finder result
fail the run.

The receipt covers merged identities, current-main blobs and published asset
digests. It does not certify historical reviews, programmed commit provenance or
the actual online audit. Read those records independently at each accepted cask
head before completing the release-observer acceptance in #7753.

An old release whose casks have since advanced should fail this check. Use the
currently delivered tag and its corresponding PRs rather than retrying stale inputs.
This workflow does not create or change a cask, release, tag or cloud resource.
