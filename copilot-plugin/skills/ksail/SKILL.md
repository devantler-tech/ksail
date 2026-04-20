---
name: ksail
description: Use the ksail CLI to spin up and manage local Kubernetes clusters (Kind/K3d/Talos/vCluster) and GitOps workloads declaratively. Triggers on requests involving local Kubernetes clusters, Flux/ArgoCD GitOps bootstrapping, Kind/K3d/Talos/vCluster, or the ksail CLI/MCP server.
---

# ksail

KSail bundles common Kubernetes tooling (kubectl, helm, kind, k3d, vcluster, flux, argocd, …) into a single Go binary. Only Docker is required externally.

Full docs: <https://ksail.devantler.tech>. Treat the docs site and `ksail <command> --help` as the source of truth; do not paraphrase flag semantics — link users to the relevant page instead.

## Prerequisites

- `ksail` on `PATH` (see <https://ksail.devantler.tech/installation/>)
- Docker daemon running (required for all local cluster providers)
- Cloud credentials only when using non-Docker providers (`HCLOUD_TOKEN` for Hetzner, `OMNI_SERVICE_ACCOUNT_KEY` for Omni)

## When to use this skill

Invoke when the user asks to:

- Create, start, stop, update, or delete a local Kubernetes cluster
- Scaffold a KSail project (`ksail.yaml`, native distribution config, `k8s/` kustomization)
- Bootstrap Flux or ArgoCD on a local cluster
- Back up or restore cluster resources
- Apply, generate, validate, or watch Kubernetes workloads
- Export/import container images for air-gapped clusters
- Manage SOPS-encrypted secrets via `ksail cipher`
- Work with the ksail MCP server or AI chat TUI

## Command groups (source of truth: `ksail --help`)

- `ksail cluster …` — lifecycle: `init`, `create`, `update`, `delete`, `start`, `stop`, `info`, `list`, `connect`, `switch`, `backup`, `restore`
- `ksail workload …` — `apply`, `create`, `edit`, `get`, `describe`, `explain`, `delete`, `logs`, `exec`, `expose`, `gen`, `validate`, `install`, `scale`, `rollout`, `wait`, `images`, `export`, `import`, `watch`, `push`, `reconcile`
- `ksail cipher …` — SOPS-based secret management
- `ksail chat` — AI chat TUI powered by GitHub Copilot
- `ksail mcp` — MCP server (already auto-registered by this plugin)

Flag-level docs live under <https://ksail.devantler.tech/cli-flags/>. Reference that page for any non-trivial flag question instead of answering from memory.

## Typical flows

Scaffold + launch a local cluster:

```bash
ksail cluster init --name my-app            # writes ksail.yaml, native config, k8s/kustomization.yaml
ksail cluster create                        # creates + starts the cluster (Docker required)
ksail cluster connect                       # opens K9s against the cluster
```

Distribution is chosen via `--distribution` (`Vanilla`, `K3s`, `Talos`, `VCluster`). Provider is `Docker` by default; Talos also supports `Hetzner` and `Omni`.

## MCP server

This plugin registers the `ksail` MCP server via `.mcp.json` (`command: ksail, args: [mcp]`). All `ksail cluster`, `ksail workload`, and `ksail cipher` commands are exposed as consolidated MCP tools (`cluster_read`, `cluster_write`, `workload_read`, `workload_write`, `cipher_write`). Prefer these tools for cluster/workload operations when running inside Copilot CLI.

## Safety

- `ksail cluster delete` destroys clusters and (with `--delete-storage`) local volumes. Confirm intent before running non-interactively.
- `ksail cluster update` may recreate clusters when immutable fields change; use `--dry-run` first.
- `ksail cipher encrypt`/`rotate` mutate files in-place; ensure they are committed before rotation.
