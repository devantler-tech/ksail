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

## Pending delivery and automatic reconciliation

Native CD gives both casks a shared twelve-minute readiness window. Exhausted
quota, temporarily unavailable evidence and unsettled checks are recorded as
**pending**, never delivered. The published immutable release is retained. Failed
checks, conflicting identity, invalid provenance or invalid signatures still fail
the handoff. Every evidence request shares the original absolute deadline,
including initial, retried and quota-recovered requests. Multiple matching open
PRs and a fully bound current-release cask with invalid download integrity are
failures; historical candidates or unavailable digest evidence cannot establish
that verdict. A later valid candidate still has to pass every existing guard.
Every native run retains `cask-handoff-result.json` for thirty days
with the release, source, run attempt, outcome and resume link.

**Reconcile Homebrew Cask Delivery** runs hourly at minute 23 or by manual dispatch
from `main`. Its read-only Ubuntu inspection skips the macOS worker when both
casks are already delivered. An open Desktop handoff verifies the downloaded
app resource seal on macOS before proceeding. The workflow shares CD concurrency so it cannot race a new tag rewriting the
evergreen branches. It derives the target from the latest published stable
immutable release and reuses the guarded exact-head finisher. It does not repair
invalid metadata or cask contents. An already-merged cask must independently pass
programmed commit provenance, immutable merge/current-main content and published
asset digest verification.

A reconciliation run succeeds only when both casks are verified delivered. Work
still pending or failing a guard makes that observer run fail with a summary, so
an undelivered cask cannot disappear behind successful release publication.
Inspect that run and the named tap PR before intervening. Do not replay a release
or manually merge a generated PR merely to produce a green result. When a newer
release supersedes a pending version, the reconciler verifies the current version.

For #8290, inspect these native and reconciliation outcomes for one week after
deployment. Record any failed/timed-out handoff followed by a later tap merge.
The one-week acceptance remains open until that observation has actually elapsed.
