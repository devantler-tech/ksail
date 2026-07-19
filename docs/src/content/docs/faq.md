---
title: FAQ
description: Frequently asked questions about KSail
---

## General Questions

### What is KSail?

KSail is a CLI tool that bundles common Kubernetes tooling into a single binary. It provides a unified interface to create clusters, deploy workloads, and operate cloud-native stacks across different Kubernetes distributions and infrastructure providers.

### Why use KSail instead of kubectl/helm/kind/k3d directly?

KSail eliminates tool sprawl by embedding kubectl, helm, kind, k3d, vcluster, flux, and argocd into one binary with a consistent workflow across distributions. It uses standard native config files (kind.yaml, k3d.yaml, vcluster.yaml), provides declarative configuration with built-in best practices, and includes GitOps integration—without vendor lock-in.

### Am I locked into KSail?

No. KSail generates native configuration files usable directly with their respective tools:

```bash
kind create cluster --config kind.yaml
k3d cluster create --config k3d.yaml
talosctl cluster create --config-patch @talos/cluster/patches.yaml
vcluster create my-cluster --values vcluster.yaml
```

### Is KSail production-ready?

KSail targets **local development, CI/CD, and learning environments**. For production, use managed services (EKS, GKE, AKS) with proper HA and security. The Talos Hetzner provider suits personal homelabs but should be evaluated carefully for production use.

## Installation & Setup

### Which operating systems does KSail support?

KSail supports Linux (amd64, arm64), macOS (arm64/Apple Silicon), and Windows (WSL2 recommended). See the [Installation Guide](/installation/) for details.

### Do I need to install Docker, kubectl, helm, etc.?

Docker is required for local cluster creation. KSail embeds kubectl, helm, kind, k3d, vcluster, flux, and argocd as Go libraries—no separate installation needed. For Hetzner cloud clusters, you need a Hetzner account and API token.

### How do I update KSail?

The update method depends on how you installed it:

```bash
brew upgrade devantler-tech/tap/ksail                  # Homebrew
go install github.com/devantler-tech/ksail/v7@latest   # Go install
# Binary: https://github.com/devantler-tech/ksail/releases
```

## Cluster Management

### Which Kubernetes distributions does KSail support?

KSail currently supports **Vanilla** (Kind), **K3s** (K3d), **Talos**, **VCluster** (Vind), **KWOK** (kwokctl, simulated), and **EKS** (cluster lifecycle and managed node-group capacity updates supported via `eksctl`; KSail-managed component installation is still in progress). See the [Support Matrix](/support-matrix/) for current provider compatibility and feature status.

### Can I create multiple clusters?

Yes. Use `ksail project init --name <name>` then `ksail cluster create` for each cluster. List all with `ksail cluster list`.

### How do I create an ephemeral cluster that auto-destroys?

Use `--ttl` with `ksail cluster create`. The process blocks until the TTL elapses, then auto-deletes the cluster and its state. Press Ctrl+C to cancel the wait and keep the cluster running.

```bash
# Cluster auto-destroys after 1 hour
ksail cluster create --ttl 1h

# Supported duration formats: 30m, 1h, 2h30m
```

For usage patterns and tips, see [Ephemeral Clusters](/guides/ephemeral-clusters/).

### How do I switch between clusters?

Use `ksail cluster switch` for the native experience. Run it without arguments for an interactive picker, or pass a cluster name directly:

```bash
ksail cluster switch          # interactive picker — recent clusters shown first (requires a TTY)
ksail cluster switch dev      # switch directly to "dev"
```

The interactive picker orders recently-switched clusters (up to the last 5) at the top, making it fast to cycle between a small set of active clusters. History is saved to `~/.ksail/switch-history.json`.

You can also use `kubectl config use-context <context-name>` directly, or list all contexts with `kubectl config get-contexts`.

### Can I use my own container registry?

Yes! KSail supports local registries with optional authentication, mirror registries to avoid rate limits, and external registries with authentication. Credentials are automatically discovered from Docker config (`~/.docker/config.json`), environment variables, or GitOps secrets. See [Registry Management](/guides/registry-management/) for configuration examples.

### What happens if I change the distribution or provider in ksail.yaml?

Changing the distribution (e.g., Vanilla to Talos) or provider (e.g., Docker to Hetzner) requires full cluster recreation. Delete the old cluster with `ksail cluster delete`, then run `ksail cluster create`.

### Which distributions support LoadBalancer services?

LoadBalancer support varies by distribution and provider:

- **Vanilla**: cloud-provider-kind
- **K3s**: built-in ServiceLB
- **Talos/Docker**: MetalLB (pool 172.18.255.200-172.18.255.250); **Talos/Hetzner**: Hetzner Cloud Load Balancer
- **VCluster**: delegates to host cluster (`spec.cluster.loadBalancer` has no effect)
- **KWOK**: simulated via API (no real traffic routing)
- **EKS**: built-in AWS Load Balancer Controller (planned)

See the [Support Matrix](/support-matrix/#component--distribution-matrix) for the full compatibility table.

### Can I add nodes to an existing cluster?

Node scaling support depends on the distribution: Talos supports both control-plane and worker nodes via `ksail cluster update`, K3s supports worker (agent) nodes only (server scaling requires recreation), EKS supports in-place `desiredCapacity`, `minSize`, and `maxSize` updates for existing managed node groups, and Vanilla (Kind) requires full recreation. See the [Update Behavior](/support-matrix/#update-behavior) table for details.

### What does `ksail cluster update --dry-run` show?

Previews all detected configuration changes without applying them. Each change is listed with an emoji classification and `old → new` values:

- 🟢 **In-place** — applied without disruption
- 🟡 **Reboot-required** — applied but requires node reboots
- 🔴 **Recreate-required** — requires full cluster recreation

Outputs `No changes detected` when configuration is already in sync.

### What happens when I run `ksail cluster update` with no changes?

It compares the current cluster state against `ksail.yaml` and exits with `No changes detected` if nothing has changed—making it safe to run frequently in CI/CD pipelines.

## Workload Management

### What's the difference between `ksail workload apply` and `ksail workload reconcile`?

`apply` deploys directly (kubectl-style, no GitOps); `reconcile` syncs via Flux or ArgoCD for Git-driven deployments.

### Can I use Helm charts with KSail?

Yes. KSail includes Helm v4. Use `ksail workload install <chart> --namespace <ns>` to install a chart, or `ksail workload gen helmrelease <name> --source=oci://registry/chart` to generate a HelmRelease for GitOps.

### How do I debug failing pods?

Use `ksail workload logs <resource>` to view logs, `ksail workload describe <resource>` to inspect resources, and `ksail workload exec <resource> -- /bin/sh` to shell into a container.

## GitOps

### Which GitOps tools does KSail support?

KSail supports both **Flux** and **ArgoCD**, chosen with `--gitops-engine Flux` or `--gitops-engine ArgoCD` during `ksail project init`.

### Do I need a Git repository for GitOps?

Not necessarily. KSail packages manifests as OCI artifacts and pushes to a local registry, enabling GitOps without Git (useful for local development):

```bash
ksail project init --gitops-engine Flux --local-registry localhost:5050
ksail cluster create
ksail workload push
ksail workload reconcile
```

To use your own Git repository, configure the GitOps engine after initialization—KSail scaffolds the initial CRs.

### Why does Flux operator installation take so long?

Flux operator CRDs can take 7-12 minutes to become established on resource-constrained systems; KSail handles this automatically with a 12-minute timeout. Ensure 4GB+ RAM is available. See [Troubleshooting - Flux Operator Installation Timeout](/troubleshooting/#flux-operator-installation-timeout) for details.

## Configuration

### What's the difference between CLI flags and ksail.yaml?

CLI flags provide quick overrides and scripting support, while ksail.yaml offers declarative configuration suitable for version control and team consistency. CLI flags override ksail.yaml values. See [Configuration Overview](/configuration/).

### Can I version control my cluster configuration?

Yes! Commit `ksail.yaml` (and generated distribution configs like kind.yaml) to Git—team members can recreate the same cluster from it.

### How do I share configurations between environments?

Use the `--config` flag to point KSail at environment-specific files:

```bash
ksail --config ksail.dev.yaml cluster create
ksail --config ksail.staging.yaml cluster update
ksail --config ksail.prod.yaml workload push
```

Alternatively, use environment variable placeholders in a shared `ksail.yaml`. For a complete walkthrough covering both approaches and CI/CD patterns, see [Multi-Environment Workflows](/guides/multi-environment/).

## Security & Secrets

### How do I manage secrets with KSail?

KSail includes **SOPS** for secret encryption via `ksail workload cipher <file>` (age, PGP, cloud KMS). See [Secret Management](/guides/secret-management/).

### Are my credentials stored securely?

KSail expands `${VAR}` syntax at runtime; credentials are never stored in config files. Example: `ksail project init --local-registry 'user:${REGISTRY_TOKEN}@registry.example.com'` (set `REGISTRY_TOKEN` before running).

## Licensing

### Why is KSail licensed under PolyForm Shield 1.0.0?

KSail was relicensed from Apache-2.0 (via GPL-3.0-only in [#4543](https://github.com/devantler-tech/ksail/pull/4543)) to [PolyForm Shield 1.0.0](https://polyformproject.org/licenses/shield/1.0.0) in [#4603](https://github.com/devantler-tech/ksail/pull/4603) (May 2026). The PolyForm Shield license allows anyone to use, modify, and distribute KSail for any purpose — except providing a competing product. This protects the project from commercial exploitation while keeping the code freely available for all other use cases.

### Is KSail open source?

KSail is **source-available**, not open source by the [OSI definition](https://opensource.org/osd). The non-compete clause disqualifies it from OSI approval. In practice, the difference only matters if you intend to build a competing product — for all other use cases (CLI tool, library, internal tooling, learning, forking), KSail works exactly like an open-source project.

### Can I use KSail in my proprietary project?

**Yes.** PolyForm Shield does not impose copyleft requirements — your project can use any license. You can use KSail as a CLI tool, embed it as a Go library, or incorporate its code into your own projects. If you redistribute KSail or derivatives, you must include the [license terms](https://polyformproject.org/licenses/shield/1.0.0) and required copyright notice, and the non-compete restriction still applies.

### What counts as "competing"?

The [PolyForm Shield license](https://polyformproject.org/licenses/shield/1.0.0) defines competition broadly: goods and services compete even across different interfaces, platforms, or programming languages, and even when provided free of charge. If you market a product as a practical substitute for KSail, it competes.

### How does the license affect contributors?

By submitting a pull request, you agree that your contribution is licensed under PolyForm Shield 1.0.0. Any third-party code included in contributions must be compatible with this license. See [CONTRIBUTING.md](https://github.com/devantler-tech/ksail/blob/main/CONTRIBUTING.md) for details.

## Troubleshooting

For common solutions, see the [Troubleshooting Guide](/troubleshooting/). To clean up all cluster and Docker resources, run `ksail cluster delete && docker system prune`. For help, visit [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions) or [GitHub Issues](https://github.com/devantler-tech/ksail/issues).
