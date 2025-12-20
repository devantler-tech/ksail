---
title: "Core Concepts"
nav_order: 5
---

# Core Concepts

Key building blocks of KSail mapped to configuration values in `ksail.yaml` and CLI flags.

## Distributions

Select with `--distribution` or `spec.distribution` in `ksail.yaml`.

### Kind

[Kind](https://kind.sigs.k8s.io/) (default) runs upstream Kubernetes in Docker containers. Configure via `kind.yaml`.

### K3d

[K3d](https://k3d.io/) wraps lightweight [K3s](https://k3s.io/) in containers, using fewer resources. Configure via `k3d.yaml`.

## Cluster Components

### Container Network Interface (CNI)

Configure via `spec.cni` or `--cni` flag.

- **`Default`** – Distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d)
- **`Cilium`** – [Cilium](https://cilium.io/) via Helm for advanced observability and eBPF policies
- **`None`** – Skip CNI for manual installation

### Container Storage Interface (CSI)

Configure via `spec.csi` or `--csi` flag.

- **`Default`** – Distribution's built-in storage. K3d includes local-path-provisioner; Kind does not
- **`LocalPathStorage`** – Explicitly install [local-path-provisioner](https://github.com/rancher/local-path-provisioner) v0.0.32. On Kind, creates default `StorageClass` in `local-path-storage` namespace. No action on K3d (already included)
- **`None`** – Skip CSI for custom controllers

**Example:**

```bash
ksail cluster init --csi LocalPathStorage
```

### Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) aggregates CPU and memory usage. **Enabled by default.**

Configure via `spec.metricsServer` or `--metrics-server` flag.

**Enable for:** HPA testing, dashboards with resource usage, CPU/memory-based alerts  
**Disable for:** Minimal resource usage, simple testing

```bash
ksail cluster create --metrics-server Disabled
```

### cert-manager

[cert-manager](https://cert-manager.io/) manages TLS certificates. **Disabled by default.**

Configure via `spec.certManager` or `--cert-manager` flag. Installs `jetstack/cert-manager` Helm chart in `cert-manager` namespace with CRDs.

```bash
ksail cluster init --cert-manager Enabled
```

## Registry Management

### Local Registry

Run a local [OCI Distribution](https://distribution.github.io/distribution/) container for image storage. **Disabled by default.**

Configure via `spec.localRegistry` or `--local-registry` flag.

**Benefits:** Faster dev loops, GitOps integration, testing image pull policies

**How it works:**

1. Initialize: `ksail cluster init --local-registry Enabled --local-registry-port 5111`
2. Registry container starts with cluster (port 5111 default)
3. Push from host: `docker tag my-api localhost:5111/my-api && docker push localhost:5111/my-api`
4. Reference in manifests: `image: ksail-registry:5000/my-api`
5. Registry removed with cluster deletion

### Mirror Registries

Proxy and cache upstream registries locally. Configure with `--mirror-registry <host>=<upstream>`.

```bash
ksail cluster init \
  --mirror-registry docker.io=https://registry-1.docker.io \
  --mirror-registry gcr.io=https://gcr.io
```

**Use cases:** Avoid rate limits, offline development, speed up CI/CD

**Note:** Authentication and TLS for upstream in development. Delete with `--delete-volumes` to clean cache.

## Secret Management

[SOPS](https://github.com/getsops/sops) integration via `ksail cipher` commands.

```bash
ksail cipher encrypt <file>    # Encrypt file
ksail cipher decrypt <file>    # Decrypt file
ksail cipher edit <file>       # Edit encrypted file
ksail cipher import <key>      # Import age private key
```

### Importing Age Keys

```bash
ksail cipher import AGE-SECRET-KEY-1ZYXWVUTSRQPONMLKJIHGFEDCBA...
```

Derives public key and installs to platform-specific location (see [SOPS docs](https://github.com/getsops/sops#usage)).

### Key Management Systems

SOPS supports: age, PGP, AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault

Configure via `.sops.yaml` in your project (see [SOPS documentation](https://github.com/getsops/sops#usage)).

## GitOps Engines

GitOps workflows via [Flux](https://fluxcd.io/) or [ArgoCD](https://argo-cd.readthedocs.io/). Configure via `spec.gitOpsEngine` or `--gitops-engine`.

- **`None`** (default) – No GitOps; use `ksail workload apply`
- **`Flux`** – Install Flux CD
- **`ArgoCD`** – Install ArgoCD

### Workflow

```bash
# 1. Initialize
ksail cluster init --gitops-engine Flux --local-registry Enabled

# 2. Create cluster
ksail cluster create

# 3. Update manifests in k8s/

# 4. Push as OCI artifact
ksail workload push

# 5. Reconcile
ksail workload reconcile
```

### Commands

**`ksail workload push`** – Package manifests from source directory as OCI artifact and push to local registry (requires local registry and GitOps engine enabled).

**`ksail workload reconcile`** – Trigger GitOps reconciliation and wait for completion.

- **Flux:** Annotates root kustomization, polls for `Ready=True`
- **ArgoCD:** Hard refresh, polls for `Synced` and `Healthy`

**Timeout:** `--timeout` flag > `spec.connection.timeout` > 5m default

```bash
ksail workload reconcile --timeout 10m
```

### Choosing an Engine

**Flux:**

- Auto-watches OCI registry for new artifacts
- Lightweight, Kubernetes-native
- CRD-based configuration

**ArgoCD:**

- Web UI for visualizing deployments
- Manual refresh for new artifacts
- Rich application management
