# 0005: Observe owner-linked children in ephemeral validation

- Status: Proposed
- Date: 2026-09-25
- Related: #7290, #5919, #5344

## Context

An operator executes arbitrary reconciliation code. Admission of its custom resource does not establish that its generated workloads satisfy project policy. Kubernetes has no universal signal proving that every controller has finished, and neither a quiet interval nor a Ready condition on an arbitrary resource establishes complete child inventory.

KSail already owns an isolated Kind lifecycle and strict admission of directly declared workloads. The ordinary offline path and admission-only mode must remain available.

## Decision

Add default-off `--ephemeral-children` to `workload validate`, requiring `--ephemeral`. After admission, wait for `--ephemeral-observation-wait` (30 seconds by default, positive and at most five minutes), then collect a bounded snapshot. Inventory has a separate two-minute ceiling within the existing shared ephemeral deadline.

The admission client's server-assigned UIDs anchor the roots. Read those roots before and after collection and reject deletion or replacement. Discover core v1 and each API group's preferred version; list every listable top-level resource with pagination. Any failed discovery, failed list, repeated pagination token, missing identity on an owned object, or inventory larger than 10,000 objects fails the pass. Virtual unowned objects such as ComponentStatus legitimately have no UID and cannot participate in the owner graph. Follow owner-reference UIDs transitively, checking group/kind/name and valid namespace scope. Names, labels, and resource creation times do not establish ancestry.

The resulting manifests are evaluated by the existing schema and CEL validators. Explicitly enabled Kyverno checks use policies and Namespace context directly declared in the selected source. Existing kind exclusions and missing-schema behavior apply. Runtime status and server bookkeeping are removed from the manifest view; object payloads remain in memory. The existing CRD schema preparation owns and cleans its private temporary files.

An explicitly requested observation with no descendants fails: it cannot demonstrate child validation. Every nonempty snapshot reports its bounded scope. Cleanup runs after collection or validation errors exactly as it does after admission errors.

## Consequences

A generated child can fail local validation even when the source manifests pass. This is a snapshot of descendants present after the configured delay, not proof of convergence, future behavior, or complete operator coverage. Short-lived children that disappear before collection are outside the snapshot. Unowned resources and descendants of custom resources supplied only inside Helm charts are outside the rooted inventory. Policies supplied only by charts are enforced by admission but are not imported into this offline policy pass.

The remaining roadmap includes operator-specific readiness contracts, richer root discovery, and scanning generated children. Those capabilities require separate evidence and delivery. A real Kind run is a release gate for this experimental increment; API simulation verifies boundaries but does not establish live controller behavior.

## References

- [Kubernetes owners and dependents](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/) defines valid namespaced and cluster-scoped ownership.
- [Kubernetes API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/#retrieving-large-results-sets-in-chunks) defines list pagination. Each resource type has its own collection snapshot; this inventory is not an atomic snapshot across all resource types.
