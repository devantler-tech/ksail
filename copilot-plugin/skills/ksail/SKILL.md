---
name: ksail
description: Use the ksail CLI to spin up and manage Kubernetes clusters (Kind/K3d/Talos/vCluster/KWOK — local via Docker; EKS — cloud via AWS) and GitOps workloads declaratively. Triggers on requests involving Kubernetes clusters, Flux/ArgoCD GitOps bootstrapping, Kind/K3d/Talos/vCluster/KWOK/EKS, multi-tenancy onboarding, OIDC authentication, or the ksail CLI/MCP server.
---

# ksail

KSail bundles common Kubernetes tooling (kubectl, helm, kind, k3d, vcluster, flux, argocd, …) into a single Go binary. Local clusters need only Docker; cloud providers also need that provider's credentials, and EKS additionally needs the `eksctl` CLI.

Full docs: <https://ksail.devantler.tech>. Treat the docs site and `ksail <command> --help` as the source of truth; do not paraphrase flag semantics — link users to the relevant page instead.

## Prerequisites

- `ksail` on `PATH` (see <https://ksail.devantler.tech/installation/>)
- Docker daemon running (required for the local `Docker` provider)
- Cloud credentials only when using non-Docker providers (`HCLOUD_TOKEN` for Hetzner, `OMNI_SERVICE_ACCOUNT_KEY` for Omni, standard AWS SDK credentials such as `aws configure` or `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_REGION` for AWS)
- `eksctl` on `PATH` for the `EKS` distribution (see <https://ksail.devantler.tech/distributions/eks/>)

## When to use this skill

Invoke when the user asks to:

- Create, start, stop, update, or delete a local Kubernetes cluster
- Scaffold a KSail project (`ksail.yaml`, native distribution config, `k8s/` kustomization)
- Bootstrap Flux or ArgoCD on a local cluster
- Back up or restore cluster resources
- Apply, generate, validate, or watch Kubernetes workloads
- Export/import container images for air-gapped clusters
- Manage SOPS-encrypted secrets via `ksail workload cipher`
- Work with the ksail MCP server or AI chat TUI

## Command groups (source of truth: `ksail --help`)

- `ksail cluster …` — lifecycle: `init`, `create`, `update`, `diff`, `delete`, `start`, `stop`, `info`, `list`, `connect`, `switch`, `backup`, `restore`, `oidc` (OIDC authentication: `get-token`)
- `ksail workload …` — `apply`, `create`, `edit`, `get`, `describe`, `explain`, `delete`, `logs`, `exec`, `expose`, `gen`, `validate`, `install`, `scale`, `rollout`, `scan`, `wait`, `images`, `export`, `import`, `watch`, `push`, `reconcile`, and `cipher` (SOPS-based secret management: `encrypt`, `decrypt`, `edit`, `import`, `rotate`)
- `ksail tenant …` — multi-tenancy: `create`, `delete`
- `ksail open …` — interfaces: `web` (browser UI), `desktop` (native app), `chat` (AI chat TUI powered by GitHub Copilot), `mcp` (MCP server — see [MCP server](#mcp-server))

Flag-level docs live under <https://ksail.devantler.tech/cli-flags/>. Reference that page for any non-trivial flag question instead of answering from memory.

## Typical flows

Scaffold + launch a local cluster:

```bash
ksail project init --name my-app            # writes ksail.yaml, native config, k8s/kustomization.yaml
ksail cluster create                        # creates + starts the cluster (Docker required)
ksail cluster connect                       # opens K9s against the cluster
```

Distribution is chosen via `--distribution` (`Vanilla`, `K3s`, `Talos`, `VCluster`, `KWOK`, `EKS`). Provider defaults to `Docker` for local distributions; Talos also supports `Hetzner` and `Omni`; EKS requires `--provider AWS` and `ksail cluster create` is not yet functional for EKS (`cluster init` only — see <https://ksail.devantler.tech/distributions/eks/>).

## MCP server

`ksail open mcp` runs a stdio MCP server. It exposes the `ksail cluster`, `ksail workload` (including the nested `ksail workload cipher` SOPS operations), project and tenant commands as consolidated MCP tools: `cluster_read`, `cluster_write`, `workload_read`, `workload_write`, `project_read`, `project_write` and `tenant_write`. Some installs of this skill register the server for you; others, such as a skills-only plugin listing, do not.

- **When the tools are available**, prefer them for cluster and workload operations.
- **When they are not**, run the same operations with the `ksail` CLI. Never block on the MCP server.
- **If the user wants the tools and the client lacks them**, first check for an existing `ksail` registration (`claude mcp list` in Claude Code, `codex mcp list` in Codex). If one is listed but disabled, enable it — in Claude Code, from the `/mcp` panel — rather than adding it again. Otherwise register the server with their agreement, for example `claude mcp add ksail -- ksail open mcp` in Claude Code or `codex mcp add ksail -- ksail open mcp` in Codex. Clients that read an `mcpServers` map take `{"mcpServers": {"ksail": {"command": "ksail", "args": ["open", "mcp"]}}}`. Per-client steps: <https://ksail.devantler.tech/integrations/ai-mcp/>.

## Safety

- `ksail cluster delete` destroys clusters and (with `--delete-storage`) local volumes. Confirm intent before running non-interactively.
- `ksail cluster update` may recreate clusters when immutable fields change; use `--dry-run` first.
- `ksail workload cipher encrypt`/`rotate` mutate files in-place; ensure they are committed before rotation.
