---
title: "Core Concepts"
nav_order: 5
---

# Core Concepts

This guide explains the building blocks behind KSail and how they map to configuration values in `ksail.yaml` and CLI flags.

## Distributions

Distributions determine how Kubernetes is packaged and run. Select with `ksail cluster init --distribution` or `spec.distribution` in `ksail.yaml`.

### Kind

[Kind](https://kind.sigs.k8s.io/) is the default distribution. It runs upstream Kubernetes inside Docker containers and mirrors production cluster behavior closely. Configure node layout, networking, and port mappings in `kind.yaml`.

### K3d

[K3d](https://k3d.io/) wraps the lightweight [K3s](https://k3s.io/) distribution in containers. It uses fewer resources while preserving core Kubernetes APIs. Configure host port mappings in `k3d.yaml`.

## Cluster Components

### Container Network Interface (CNI)

The CNI determines how pods receive IP addresses and communicate. Configure via `spec.cni` in `ksail.yaml` or `--cni` flag.

**Available options:**

- **`Default`** – Uses distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d). Best for quick iterations and CI.
- **`Cilium`** – Installs [Cilium](https://cilium.io/) via Helm. Choose when you need advanced observability, eBPF-based policies, or WireGuard encryption.
- **`None`** – Skips CNI installation. Use when installing a different CNI manually.

### Container Storage Interface (CSI)

Storage options determine how persistent volumes are provisioned. Configure via `spec.csi` in `ksail.yaml` or `--csi` flag.

**Available options:**

- **`Default`** – Uses distribution's built-in storage class. K3d includes [local-path-provisioner](https://github.com/rancher/local-path-provisioner) by default, while Kind does not have a default storage class. Works well for simple development scenarios.
- **`LocalPathStorage`** – Explicitly installs [local-path-provisioner](https://github.com/rancher/local-path-provisioner) version v0.0.32. On Kind clusters, KSail installs the provisioner into the `local-path-storage` namespace and creates a `StorageClass` named `local-path` marked as the cluster default, ensuring PersistentVolumeClaims bind without additional configuration. On K3d clusters, KSail performs no action as K3d already includes local-path-provisioner.
- **`None`** – Skips CSI installation for custom storage controllers. Leaves PersistentVolumeClaims pending until your custom solution handles them.

**Example:**

```bash
ksail cluster init --csi LocalPathStorage
ksail cluster create
```

### Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) aggregates CPU and memory usage across the cluster. **Enabled by default.**

Configure via `spec.metricsServer` in `ksail.yaml` or `--metrics-server` flag.

**When to enable:**

- Testing Horizontal Pod Autoscaler (HPA)
- Using dashboard tools that display resource usage
- Working with alerts based on CPU/memory metrics

**When to disable:**

- Minimal resource consumption during development
- Simple testing that doesn't require metrics

**Example:**

```bash
ksail cluster init --metrics-server Enabled  # default
ksail cluster create --metrics-server Disabled  # override
```

### cert-manager

[cert-manager](https://cert-manager.io/) is a Kubernetes controller for issuing and renewing TLS certificates. **Disabled by default.**

Configure via `spec.certManager` in `ksail.yaml` or `--cert-manager` flag.

When enabled, KSail installs the Helm chart `jetstack/cert-manager` into the `cert-manager` namespace with `installCRDs: true`.

**Example:**

```bash
ksail cluster init --cert-manager Enabled
ksail cluster create
ksail workload get pods -n cert-manager
```

## Registry Management

### Local Registry

KSail can run a local [OCI Distribution](https://distribution.github.io/distribution/) container to store images. **Disabled by default.**

Configure via `spec.localRegistry` in `ksail.yaml` or `--local-registry` flag.

**Why use a local registry:**

- **Faster dev loops** – Push locally built images and reference them in manifests
- **GitOps integration** – Controllers can pull from local registry like in production
- **Testing** – Validate image pull policies and registry behavior locally

**How it works:**

1. Initialize with `--local-registry Enabled --local-registry-port 5111`
2. Create cluster starts a `registry:3` container connected to cluster network
3. Tag and push images from host: `docker tag my-api localhost:5111/my-api && docker push localhost:5111/my-api`
4. Reference in manifests using container name: `image: ksail-registry:5000/my-api`
5. Delete cluster tears down registry container

**Configuration:**

```bash
ksail cluster init --local-registry Enabled --local-registry-port 5111
```

The registry listens on port `5111` by default. Change with `--local-registry-port`.

### Mirror Registries

Mirror registries proxy upstream container registries (e.g., `docker.io`) and cache content locally. Configure with `--mirror-registry <host>=<upstream>` flags.

**Workflow:**

```bash
# Initialize with mirrors
ksail cluster init \
  --mirror-registry docker.io=https://registry-1.docker.io \
  --mirror-registry gcr.io=https://gcr.io

# Create cluster starts mirror containers
ksail cluster create

# Images pulled from mirrored registries are cached locally
# Delete with --delete-volumes to clean up cache
ksail cluster delete --delete-volumes
```

**Use cases:**

- **Rate limit avoidance** – Cache frequently pulled images to avoid Docker Hub rate limits
- **Offline development** – Work with previously pulled images when disconnected
- **CI/CD pipelines** – Speed up image pulls in automated testing

**Current limitations:**

- Authentication to upstream registries not yet fully supported
- TLS configuration for upstream connections in development
- Mirrors are always provisioned as local containers

## Secret Management

KSail integrates [SOPS](https://github.com/getsops/sops) for encrypting manifests through the `ksail cipher` commands.

**Available commands:**

```bash
ksail cipher encrypt <file>    # Encrypt a file with SOPS
ksail cipher decrypt <file>    # Decrypt a file with SOPS
ksail cipher edit <file>       # Edit an encrypted file with SOPS
ksail cipher import <key>      # Import age private key to default SOPS location
```

### Importing Age Keys

The `ksail cipher import` command simplifies age key management:

```bash
# Import an age private key
ksail cipher import AGE-SECRET-KEY-1ZYXWVUTSRQPONMLKJIHGFEDCBA...

# Automatically derives public key and installs to platform-specific location:
# - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
# - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
# - Windows: %AppData%\sops\age\keys.txt
```

### Key Management Systems

SOPS supports multiple key management systems:

- age recipients (recommended for local development)
- PGP fingerprints
- AWS KMS, GCP KMS, Azure Key Vault
- HashiCorp Vault

Configure SOPS using a `.sops.yaml` file in your project directory. See the [SOPS documentation](https://github.com/getsops/sops#usage) for details.

> **Note:** Full GitOps integration with automatic decryption is planned when Flux support is added.
