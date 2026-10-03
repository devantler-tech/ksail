# Recover EKS ownership without creation state

Status: Proposed. Implements [#6399](https://github.com/devantler-tech/ksail/issues/6399).

## Problem

`eks-bind` records an immutable EKS identity, but CLI and local API lifecycle paths also require
the create-time `spec.json`. Losing that file makes a successfully rebound cluster unusable. The
snapshot is sanitized configuration, not an identity record or a safe runtime configuration.

## Decision

Use the existing region-scoped ownership record as sufficient local intent for lifecycle operations.
Do not reconstruct `spec.json`, copy its redacted settings into a provisioner, or weaken live identity
verification. Existing unreadable or conflicting creation state still refuses the operation.

The experimental `eks-bind` command remains the single recovery entry point. With no project or
creation state, require explicit `--name` and `--provider AWS`. Resolve the region through the existing
AWS selection: the configured region environment variable, a matching kubeconfig context, or the
selected AWS profile. The documented recovery command sets `AWS_REGION` explicitly. Read-only eksctl,
STS and EKS queries must agree on the target and its provenance. Display account, ARN, region and
creation time; only `--yes` writes the local identity record. No cloud resource is changed.

After recovery, the CLI restores the record's credential-variable mapping and region using its
existing explicit-target resolution. The local API accepts exactly one recorded region when its
rendered `eks.yaml` is missing and rebuilds only that lifecycle config. Ambient region changes cannot
redirect it. Multiple recorded regions, corrupt records, conflicting rendered configuration, or a
live account/ARN/creation-time mismatch still refuse the action.

A recovered record also prevents a fresh create from overwriting that target and prevents failed-create
cleanup from discarding it as unowned. Rebinding clears old node-group capacity snapshots; it cannot
recover the pre-stop size of a node group that was already stopped when all local state was lost.

## Consequences

Recovery works from a new machine without inventing a create-time snapshot. Legacy creation state
remains supported, but grants no exemption from the immutable identity verifier. The recovery command
stays experimental; live AWS validation and graduation are separate from hermetic lifecycle tests.
