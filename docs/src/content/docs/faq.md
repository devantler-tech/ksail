---
title: FAQ
description: Frequently asked questions about KSail
---

## General Questions

### What is KSail?

KSail is a CLI tool that bundles common Kubernetes tooling into a single binary. It provides a unified interface to create clusters, deploy workloads, and operate cloud-native stacks across different Kubernetes distributions and infrastructure providers.

### Why use KSail instead of kubectl/helm/kind/k3d directly?

KSail eliminates tool sprawl by embedding kubectl, helm, kind, k3d, vcluster, flux, and argocd into one binary with a consistent workflow across distributions. It works with standard native configuration files (kind.yaml, k3d.yaml, vcluster.yaml) and provides declarative configuration with built-in best practices. GitOps integration is included without manual setup—all without vendor lock-in.

### Am I locked into KSail?

No. KSail generates native configuration files you can use directly with their respective tools at any time:

```bash
kind create cluster --config kind.yaml          # Vanilla
k3d cluster create --config k3d.yaml            # K3s
talosctl cluster create --config-patch @talos/cluster/patches.yaml  # Talos
vcluster create my-cluster --values vcluster.yaml  # VCluster
```

### Is KSail production-ready?

KSail targets **local development, CI/CD, and learning environments**. For production, use managed services (EKS, GKE, AKS) with proper HA and security. The Hetzner provider suits personal homelabs but should be evaluated carefully for production use.

## Installation & Setup

### Which operating systems does KSail support?

KSail works on:

- **Linux** (amd64, arm64)
- **macOS** (arm64 - Apple Silicon)
- **Windows** (WSL2 recommended, native support untested)

See the [Installation Guide](/installation/) for details.

### Do I need to install Docker, kubectl, helm, etc.?

Docker is required for local cluster creation. KSail embeds kubectl, helm, kind, k3d, vcluster, flux, and argocd as Go libraries—no separate installation needed. For Hetzner cloud clusters, you need a Hetzner account and API token.

### How do I update KSail?

The update method depends on how you installed it:

```bash
# Homebrew
brew upgrade devantler-tech/tap/ksail

# Go install
go install github.com/devantler-tech/ksail/v5@latest

# Binary download
# Download latest from https://github.com/devantler-tech/ksail/releases
```

## Cluster Management

### Which Kubernetes distributions does KSail support?

KSail supports four distributions:

- **Vanilla** (via Kind) - Upstream Kubernetes
- **K3s** (via K3d) - Lightweight Kubernetes
- **Talos** - Immutable Kubernetes OS
- **VCluster** (via Vind) - Virtual clusters in Docker

See the [Support Matrix](/support-matrix/) for provider compatibility.

### Can I create multiple clusters?

Yes! Use the `--name` flag to create multiple clusters:

```bash
ksail cluster init --name dev-cluster
ksail cluster create

ksail cluster init --name staging-cluster
ksail cluster create
```

List all clusters with `ksail cluster list`.

### How do I switch between clusters?

KSail automatically configures your kubeconfig with the appropriate context. Use standard kubectl context switching:

```bash
# List contexts
kubectl config get-contexts

# Switch context
kubectl config use-context <cluster-name>
```

### Can I use my own container registry?

Yes! KSail supports local registries with optional authentication, mirror registries to avoid rate limits, and external registries with authentication. Credentials are automatically discovered from Docker config (`~/.docker/config.json`), environment variables, or GitOps secrets. See [Registry Management](/features/#registry-management) for configuration examples.

### What happens if I change the distribution or provider in ksail.yaml?

Changing the distribution (e.g., Vanilla to Talos) or provider (e.g., Docker to Hetzner) requires full cluster recreation. Delete the old cluster with `ksail cluster delete`, then run `ksail cluster create`.

### Which distributions support LoadBalancer services?

All distributions provide LoadBalancer support. Vanilla (Kind) uses cloud-provider-kind, K3s uses built-in ServiceLB, Talos on Docker uses MetalLB (IP pool 172.18.255.200-172.18.255.250), Talos on Hetzner uses Hetzner Cloud Load Balancer, and VCluster delegates LoadBalancer to the host cluster. The `spec.cluster.loadBalancer` setting has no effect on VCluster clusters—KSail does not install or uninstall any LoadBalancer controller for VCluster.

### Can I add nodes to an existing cluster?

Node scaling support depends on the distribution: Talos supports both control-plane and worker nodes via `ksail cluster update`, K3s supports worker (agent) nodes only (server scaling requires recreation), and Vanilla (Kind) requires full recreation. See the [Update Behavior](/support-matrix/#update-behavior) table for details.

### What does `ksail cluster update --dry-run` show?

Previews all detected configuration changes without applying them, including change classifications (in-place, reboot-required, or recreate-required) and a summary of impacts.

### What happens when I run `ksail cluster update` with no changes?

It compares the current cluster state against `ksail.yaml` and exits with `No changes detected` if nothing has changed—making it safe to run frequently in CI/CD pipelines.

## Workload Management

### What's the difference between `ksail workload apply` and `ksail workload reconcile`?

`apply` deploys directly (kubectl-style, no GitOps); `reconcile` syncs via Flux or ArgoCD for Git-driven deployments.

### Can I use Helm charts with KSail?

Yes! KSail includes Helm v4:

```bash
ksail workload install <chart> --namespace <ns>             # Install chart
ksail workload gen helmrelease <name> --source=oci://registry/chart  # Generate HelmRelease for GitOps
```

### How do I debug failing pods?

KSail wraps kubectl debugging commands:

```bash
ksail workload logs deployment/my-app            # View logs
ksail workload describe pod/my-pod               # Describe resource
ksail workload exec deployment/my-app -- /bin/sh # Execute in container
```

## GitOps

### Which GitOps tools does KSail support?

KSail supports both **Flux** and **ArgoCD**, chosen with `--gitops-engine Flux` or `--gitops-engine ArgoCD` during `ksail cluster init`.

### Do I need a Git repository for GitOps?

Not necessarily! KSail can package manifests as OCI artifacts and push to a local registry, enabling GitOps workflows without Git (useful for local development):

```bash
ksail cluster init --gitops-engine Flux --local-registry localhost:5050
ksail cluster create
ksail workload push      # Package and push
ksail workload reconcile # Sync to cluster
```

### Can I use my own Git repository?

Yes! After initialization, configure your GitOps engine to point to your Git repository. KSail scaffolds the initial CRs, but you customize them to use your repository.

### Why does Flux operator installation take so long?

Flux operator installation can take 7-12 minutes on resource-constrained systems due to CRD establishment delays. KSail handles this automatically with a 12-minute timeout. Monitor progress with `ksail workload get pods -n flux-system` or `kubectl get crds | grep fluxcd.io`. For faster installations, ensure 4GB+ RAM is available. See [Troubleshooting - Flux Operator Installation Timeout](/troubleshooting/#flux-operator-installation-timeout) for details.

## Configuration

### What's the difference between CLI flags and ksail.yaml?

CLI flags provide quick overrides and scripting support, while ksail.yaml offers declarative configuration suitable for version control and team consistency. CLI flags override ksail.yaml values. See [Configuration Overview](/configuration/).

### Can I version control my cluster configuration?

Yes! Commit `ksail.yaml` (and generated distribution configs like kind.yaml) to Git—team members can recreate the same cluster from it.

### How do I share configurations between environments?

Use environment-specific files (`ksail-dev.yaml`, `ksail-staging.yaml`, `ksail-prod.yaml`) or environment variable placeholders in a shared `ksail.yaml`.

## Security & Secrets

### How do I manage secrets with KSail?

KSail includes **SOPS** for secret encryption via `ksail cipher encrypt|decrypt|edit <file>`. Supports age, PGP, and cloud KMS providers. See [Secret Management](/features/#secret-management).

### Are my credentials stored securely?

KSail uses environment variables for sensitive data (`${VAR}` syntax in configuration). Values are expanded at runtime and never stored in config files:

```bash
export REGISTRY_TOKEN="my-secret-token"
ksail cluster init --local-registry 'user:${REGISTRY_TOKEN}@registry.example.com'
```

## Troubleshooting

### My cluster creation hangs - what should I do?

See the [Troubleshooting Guide](/troubleshooting/#cluster-creation-hangs) for common solutions.

### How do I clean up resources?

```bash
ksail cluster delete  # Removes containers/VMs and Kubernetes resources
docker system prune   # Clean up dangling Docker resources
```

### Where can I get help?

- **Documentation:** [ksail.devantler.tech](https://ksail.devantler.tech)
- **Troubleshooting:** [Troubleshooting Guide](/troubleshooting/)
- **Discussions:** [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)
- **Issues:** [GitHub Issues](https://github.com/devantler-tech/ksail/issues)
