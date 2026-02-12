---
title: FAQ
description: Frequently asked questions about KSail
---

## General Questions

### What is KSail?

KSail is a CLI tool that bundles common Kubernetes tooling into a single binary. It provides a unified interface to create clusters, deploy workloads, and operate cloud-native stacks across different Kubernetes distributions and infrastructure providers.

### Why use KSail instead of kubectl/helm/kind/k3d directly?

KSail eliminates tool sprawl by embedding kubectl, helm, kind, k3d, flux, and argocd into one binary. You get:

- **Consistent workflow** across distributions (Vanilla, K3s, Talos)
- **Declarative configuration** for reproducible environments
- **Built-in best practices** for CNI, CSI, observability, and security
- **GitOps integration** without manual setup
- **One tool to learn** instead of many

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

**Docker is required** for local cluster creation (the Docker provider). KSail embeds kubectl, helm, kind, k3d, flux, and argocd as Go libraries, so you don't need to install them separately.

For Hetzner cloud clusters (Talos only), you need a Hetzner account and API token, but Docker is still used for the KSail binary.

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

KSail supports three distributions:

- **Vanilla** (via Kind) - Upstream Kubernetes
- **K3s** (via K3d) - Lightweight Kubernetes
- **Talos** - Immutable Kubernetes OS

See the [Support Matrix](/support-matrix/) for provider compatibility.

### Can I create multiple clusters?

Yes! Use the `--name` flag to create multiple clusters:

```bash
ksail cluster init --name dev-cluster
ksail cluster create

ksail cluster init --name staging-cluster
ksail cluster create
```

List all clusters with `ksail cluster list --all`.

### How do I switch between clusters?

KSail automatically configures your kubeconfig with the appropriate context. Use standard kubectl context switching:

```bash
# List contexts
kubectl config get-contexts

# Switch context
kubectl config use-context <cluster-name>
```

### Can I use my own container registry?

Yes! KSail supports:

1. **Local registry** - Runs on localhost with optional authentication
2. **Mirror registries** - Proxy to upstream registries (avoid rate limits)
3. **External registries** - Use your own registry with authentication

See [Registry Management](/features/#registry-management) for examples.

### What happens if I change the distribution or provider in ksail.yaml?

Changing the distribution (e.g., Vanilla to Talos) or provider (e.g., Docker to Hetzner) requires full cluster recreation.
The current implementation does not automatically detect distribution/provider changes on an existing cluster.
You must manually delete the old cluster first with `ksail cluster delete`, then run `ksail cluster create`.

### Which distributions support LoadBalancer services?

LoadBalancer support varies by distribution and provider:

| Distribution         | Support Status  | LoadBalancer Implementation     | Configuration Required    |
|----------------------|-----------------|---------------------------------|---------------------------|
| **Vanilla (Kind)**   | ✅ Supported     | MetalLB                         | `--load-balancer Enabled` |
| **K3s (K3d)**        | ✅ Built-in      | ServiceLB (Klipper-LB)          | Enabled by default        |
| **Talos on Docker**  | ❌ Not supported | N/A                             | Always `Disabled`         |
| **Talos on Hetzner** | ✅ Built-in      | hcloud-cloud-controller-manager | Enabled by default        |

**Quick setup examples:**

``````bash
# Vanilla with LoadBalancer
ksail cluster init --distribution Vanilla --load-balancer Enabled

# K3s with LoadBalancer (default)
ksail cluster init --distribution K3s

# Talos on Hetzner with LoadBalancer (default)
export HCLOUD_TOKEN="your-token"
ksail cluster init --distribution Talos --provider Hetzner
``````

**Talos on Docker limitations:**

MetalLB support for Talos+Docker is planned for a future release. Until then, LoadBalancer services are not supported on this combination. The `--load-balancer` flag is automatically set to `Disabled`, regardless of user input.

**Workarounds for Talos+Docker:**

``````bash
# Option 1: Use NodePort
apiVersion: v1
kind: Service
metadata:
  name: my-app
spec:
  type: NodePort
  selector:
    app: my-app
  ports:
    - port: 80
      targetPort: 8080
      nodePort: 30080  # Access via localhost:30080

# Option 2: Use ClusterIP with port-forwarding
ksail workload port-forward svc/my-app 8080:80
``````

See the [LoadBalancer Configuration Guide](/configuration/loadbalancer/) for complete setup instructions and examples.

### Can I add nodes to an existing cluster?

It depends on the distribution:

- **Talos** - Yes, both control-plane and worker nodes can be scaled via `ksail cluster update`
- **K3s (K3d)** - Yes, worker (agent) nodes can be added/removed. Server (control-plane) scaling requires recreation
- **Vanilla (Kind)** - No, Kind does not support node changes after creation. Requires recreation

See the [Update Behavior](/support-matrix/#update-behavior) table for details.

### What does `ksail cluster update --dry-run` show?

The `--dry-run` flag previews all detected configuration changes without applying them.
It lists each change with its classification (in-place, reboot-required, or recreate-required)
and a summary of how many changes would be applied. This is useful for reviewing the impact before committing.

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

Not necessarily! KSail can package manifests as **OCI artifacts** and push to a local registry:

```bash
ksail cluster init --gitops-engine Flux --local-registry
ksail cluster create
ksail workload push      # Package and push to local registry
ksail workload reconcile # Sync to cluster
```

This enables GitOps workflows without Git (useful for local development).

### Can I use my own Git repository?

Yes! After initialization, configure your GitOps engine to point to your Git repository. KSail scaffolds the initial CRs, but you customize them to use your repository.

## Configuration

### What's the difference between CLI flags and ksail.yaml?

Both configure KSail:

- **CLI flags** - Quick overrides, one-off changes, scripting
- **ksail.yaml** - Declarative config, version-controlled, team consistency

CLI flags override `ksail.yaml` values. See [Configuration Overview](/configuration/).

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

KSail uses environment variables for sensitive data (`${VAR}` syntax in configuration). The values are expanded at runtime and never stored in config files.

```bash
# Set credential
export REGISTRY_TOKEN="my-secret-token"

# Use in configuration
ksail cluster init \
  --local-registry 'user:${REGISTRY_TOKEN}@registry.example.com'
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
