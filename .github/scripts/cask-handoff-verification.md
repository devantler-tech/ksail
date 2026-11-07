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

For #7847, optionally enable `native_cli_trial` (off by default). After the
metadata verifier succeeds, a separate disposable macOS arm64 job consumes its
exact same-run artifact. It pins the tap commit and cask blob from that receipt,
checks the expected release version and native asset digest, installs only the
CLI cask, and observes a literal Tab through an interactive zsh terminal. The
temporary zsh profile uses normal `brew shellenv zsh` and `compinit`; it never
sources KSail's completion directly or changes the maintainer's Mac profiles.

The trial records success only after uninstalling its owned cask and observing
cleanup. If the evergreen tap advances after verification, the trial still tests
the receipt's immutable version. Its receipt names that tested version; it is
not a claim about a later release. A fixture pass or the metadata receipt alone
does not satisfy the fresh published-install and Tab acceptance.
