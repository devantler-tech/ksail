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

No. KSail generates native distribution configuration files that work directly with underlying tools. Use the generated kind.yaml, k3d.yaml, Talos patches, or vcluster.yaml with their respective CLI tools at any time. You can migrate away or use both KSail and native tools interchangeably.

### Can I use KSail configs with native tools?

Yes! After running `ksail cluster init`, you can use the generated configuration files directly with native tools:

```bash
kind create cluster --config kind.yaml          # Vanilla
k3d cluster create --config k3d.yaml            # K3s
talosctl cluster create --config-patch @talos/cluster/patches.yaml  # Talos
vcluster create my-cluster --values vcluster.yaml  # VCluster
```

### Is KSail production-ready?

KSail is designed for **local development, CI/CD, and learning environments**. For production Kubernetes clusters, we recommend using distribution-specific tools or managed Kubernetes services (EKS, GKE, AKS) with proper HA, backup, and security configurations.

The Hetzner provider for Talos is suitable for personal homelabs and development environments, but should be evaluated carefully for production use.

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

All distributions provide LoadBalancer support. Vanilla (Kind) uses cloud-provider-kind, K3s uses built-in ServiceLB, Talos on Docker uses MetalLB (IP pool 172.18.255.200-250), Talos on Hetzner uses Hetzner Cloud Load Balancer, and VCluster manages it internally.

### Can I add nodes to an existing cluster?

Node scaling support depends on the distribution: Talos supports both control-plane and worker nodes via `ksail cluster update`, K3s supports worker (agent) nodes only (server scaling requires recreation), and Vanilla (Kind) requires full recreation. See the [Update Behavior](/support-matrix/#update-behavior) table for details.

### What does `ksail cluster update --dry-run` show?

Previews all detected configuration changes without applying them, including change classifications (in-place, reboot-required, or recreate-required) and a summary of impacts.

### What happens when I run `ksail cluster update` with no changes?

The command compares the current cluster state against your `ksail.yaml` configuration. If no differences are detected, it prints `No changes detected` and exits without applying any changes, so no cluster modifications are made. This makes `ksail cluster update` safe to run frequently or in CI/CD pipelines; it is a no-op when the cluster is already in the desired state.

## Workload Management

### What's the difference between `ksail workload apply` and `ksail workload reconcile`?

- **`ksail workload apply`** - Direct kubectl-style deployment (no GitOps)
- **`ksail workload reconcile`** - GitOps workflow (requires Flux or ArgoCD)

Use `apply` for quick iteration, `reconcile` for Git-driven deployments.

### Can I use Helm charts with KSail?

Yes! KSail includes Helm v4 with kstatus:

```bash
# Install a Helm chart
ksail workload install <chart> --namespace <ns>

# Generate HelmRelease for GitOps
ksail workload gen helmrelease <name> --source=oci://registry/chart
```

### How do I debug failing pods?

KSail wraps kubectl debugging commands:

```bash
# View logs
ksail workload logs deployment/my-app

# Describe resource
ksail workload describe pod/my-pod

# Execute in container
ksail workload exec deployment/my-app -- /bin/sh
```

## GitOps

### Which GitOps tools does KSail support?

KSail supports both **Flux** and **ArgoCD**. Choose during initialization:

```bash
ksail cluster init --gitops-engine Flux
# or
ksail cluster init --gitops-engine ArgoCD
```

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

Yes! The `ksail.yaml` file is designed for Git:

```bash
# Initialize project
ksail cluster init --distribution Vanilla --cni Cilium

# Commit configuration
git add ksail.yaml kind.yaml k8s/
git commit -m "chore: initial cluster configuration"
```

Team members can recreate the same cluster from `ksail.yaml`.

### How do I share configurations between environments?

Use environment-specific `ksail.yaml` files:

```
myproject/
├── ksail-dev.yaml
├── ksail-staging.yaml
└── ksail-prod.yaml
```

Or use environment variables with placeholders in `ksail.yaml`.

## Security & Secrets

### How do I manage secrets with KSail?

KSail includes **SOPS** for secret encryption:

```bash
# Encrypt a file
ksail cipher encrypt secret.yaml

# Decrypt a file
ksail cipher decrypt secret.enc.yaml

# Edit encrypted file
ksail cipher edit secret.enc.yaml
```

Supports age, PGP, and cloud KMS providers. See [Secret Management](/features/#secret-management).

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
# Delete a cluster (removes containers/VMs and resources)
ksail cluster delete

# Clean up Docker resources
docker system prune

# For Hetzner, deletion removes cloud resources automatically
```

### Where can I get help?

- **Documentation:** [ksail.devantler.tech](https://ksail.devantler.tech)
- **Troubleshooting:** [Troubleshooting Guide](/troubleshooting/)
- **Discussions:** [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)
- **Issues:** [GitHub Issues](https://github.com/devantler-tech/ksail/issues)
