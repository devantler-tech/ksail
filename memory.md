# Repo Assist Memory

## Last Run
2026-05-20 01:17 UTC — Run #26135227761

## Monthly Activity Issue
- May 2026: #4501 (open)

## Open Repo Assist PRs
- #4795: feat(picker): add / filter mode + ParseTags fast-path optimisation (CI mostly passing)
- #4781: test(image): add direct unit tests for isHelperContainer helper (CI passing)

## Issue Comments Made (with run)
- #4328 (EKS provider status): 2026-05-20
- #4674 (Hetzner CCM taint race): 2026-05-12
- #4423 (workload watch hooks): 2026-05-11
- #4675 (Talos disk encryption): 2026-05-10
- #4646 (Hetzner label + ~ bugs): 2026-05-08
- #4354 (KWOK v0.7.0): 2026-05-07
- #4621 (Hetzner node role bug): 2026-05-06
- #3912 (Dependabot multi-arch): 2026-05-05
- #4474 (cluster diff): 2026-05-13
- #3983 (Hetzner K3s/Vanilla): 2026-05-18
- #4768 (k8s provider docs): 2026-05-17

## Issues Created (last 4 weeks)
- #4627: [Parent] Cloud Provider Expansion (groups #3983, #4328, #4510) — all 3 linked
- #4610: [ci] fix K3s+Cilium timing race — blocked (issue #4681)
- #4602: [feature] OIDC federation
- #4521: [feature] local-remote mirroring
- #4511: [feature] native ephemeral CI mode
- #4510: [chore] GKE/AKS providers (sub-issue of #4627)
- #4474: cluster diff command
- #4473: vCluster v0.34 compat (merged #4734)
- #4465: cluster switch fuzzy search (partially addressed by #4795)
- #4423: workload watch hooks
- #4422: cluster diagnose health scoring
- #4375: cluster graph topology
- #3983: Hetzner K3s/Vanilla (sub-issue of #4627)
- #4777: [feature] DevContainer scaffolding
- #4778: [feature] eBPF traffic observability via Hubble

## Sub-Issue Links (verified)
- #4627 parent: #4328, #4510, #3983 (all linked, all open as of 2026-05-20)

## Code Quality Domain Alternation
- Last domain: performance (Task 8, 2026-05-19)
- Next for Task 5: refactor

## Backlog Cursor
- Issues processed through #4795 (as of 2026-05-20)
- Roadmap from discussion #4776 (May 18, 2026): all items tracked

## Labels Applied (recent)
- #4743, #4432, #4371 → `agentic-workflows`, `automation` (May 19)
- #4501 → `automation`, `repo-assist` (May 12)

## Notes
- link_sub_issue is NOT idempotent — always verify sub-issue not already linked before calling
- Weekly strategy #4776 (May 18, 2026) is the current roadmap reference
- Issue #4675 CLOSED (completed) — Talos disk encryption implemented in #4676
- Issue #4316 CLOSED (completed) — ArgoCD v3.4 done
- Issue #4473 CLOSED — vCluster v0.34 done (merged #4734)
- Issue #4681: CI workload-timeout (blocked by workflows permission, needs manual PR creation)
- PR #4769 (fix kubeconfig context): human PR by devantler, open for review
- PR #4794 (toolgen docs): human PR by devantler, open for review
- Duplicate patterns in v1alpha1 enum types are intentional (nolint:dupl) — do NOT refactor Set() methods
- EKS provider is largely implemented; missing: CI system test + docs 🚧 removal (#4328)
- k3k Dependabot PR #4793: CI has infrastructure failures (not code-related)
