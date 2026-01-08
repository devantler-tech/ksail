---
title: "Features"
nav_order: 4
---

# Features

KSail provides a unified CLI for local Kubernetes development with built-in support for cluster provisioning, workload management, GitOps workflows, and secret encryption.

## Cluster Provisioning

Create and manage local Kubernetes clusters with a single command. KSail supports multiple distributions and automatically configures networking, storage, and observability components.

```bash
ksail cluster init --distribution Kind --cni Cilium
ksail cluster create
```

**Capabilities:**

- **Multi-distribution support** – [Kind, K3d, and Talos](concepts.md#distributions) distributions
- **Component installation** – [CNI](concepts.md#container-network-interface-cni) (Cilium, Calico), [CSI](concepts.md#container-storage-interface-csi), [metrics-server](concepts.md#metrics-server) with [kubelet-csr-approver](concepts.md#kubelet-csr-approver), [cert-manager](concepts.md#cert-manager), and [policy engines](concepts.md#policy-engines)
- **Node configuration** – Control-plane and worker node counts via `--control-planes` and `--workers`
- **Connection management** – Automatic kubeconfig and context configuration

**Commands:** [`ksail cluster`](configuration/cli-flags/cluster/cluster-root.md)

| Command                 | Description                 |
| ----------------------- | --------------------------- |
| `ksail cluster init`    | Initialize a new project    |
| `ksail cluster create`  | Create a cluster            |
| `ksail cluster delete`  | Delete a cluster            |
| `ksail cluster start`   | Start a stopped cluster     |
| `ksail cluster stop`    | Stop a running cluster      |
| `ksail cluster info`    | Show cluster information    |
| `ksail cluster list`    | List clusters               |
| `ksail cluster connect` | Connect to cluster with K9s |

**Configuration:** [Declarative Configuration](configuration/declarative-configuration.md)

## Workload Management

Deploy and manage Kubernetes workloads using familiar kubectl and Helm patterns wrapped in consistent commands.

```bash
ksail workload apply -k k8s/
ksail workload get pods
ksail workload logs deployment/my-app
```

**Capabilities:**

- **Apply manifests** – [Kustomize](concepts.md#kustomize) directories, [Helm](concepts.md#helm) charts, or raw YAML
- **Generate resources** – Create deployments, services, secrets, and more
- **Debug workloads** – Logs, exec, describe, and explain commands
- **Validate manifests** – Schema validation before applying

**Commands:** [`ksail workload`](configuration/cli-flags/workload/workload-root.md)

| Command                   | Description                        |
| ------------------------- | ---------------------------------- |
| `ksail workload apply`    | Apply manifests to cluster         |
| `ksail workload get`      | Get resources                      |
| `ksail workload describe` | Describe resources                 |
| `ksail workload logs`     | View container logs                |
| `ksail workload exec`     | Execute command in container       |
| `ksail workload gen`      | Generate Kubernetes manifests      |
| `ksail workload validate` | Validate manifests against schemas |
| `ksail workload install`  | Install Helm charts                |
| `ksail workload scale`    | Scale deployments                  |
| `ksail workload rollout`  | Manage rollouts                    |
| `ksail workload wait`     | Wait for conditions                |

## GitOps Workflows

Enable GitOps with [Flux or ArgoCD](concepts.md#gitops) for declarative, Git-driven deployments. KSail scaffolds the required CRs and provides commands for pushing and reconciling workloads.

```bash
ksail cluster init --gitops-engine Flux --local-registry Enabled
ksail cluster create
ksail workload push
ksail workload reconcile
```

**Capabilities:**

- **Engine installation** – Automatic Flux or ArgoCD setup
- **CR scaffolding** – FluxInstance or ArgoCD Application generated automatically
- **OCI artifact packaging** – Package manifests and push to local registry
- **Reconciliation triggers** – Force sync and wait for completion

**Workflow:**

1. Initialize with GitOps engine and local registry enabled
2. Create cluster (installs GitOps controllers)
3. Edit manifests in source directory
4. Push manifests as OCI artifact
5. Trigger reconciliation

**Commands:**

| Command                    | Description                            |
| -------------------------- | -------------------------------------- |
| `ksail workload push`      | Package and push manifests to registry |
| `ksail workload reconcile` | Trigger GitOps sync and wait           |

## Registry Management

Run local [OCI registries](concepts.md#oci-registries) for faster development cycles and configure mirror registries to avoid rate limits.

### Local Registry

```bash
ksail cluster init --local-registry Enabled --local-registry-port 5111
ksail cluster create

docker tag my-app localhost:5111/my-app
docker push localhost:5111/my-app
```

**Benefits:** Faster image pulls, GitOps integration, isolated development

### Mirror Registries

```bash
ksail cluster init \
  --mirror-registry docker.io=https://registry-1.docker.io \
  --mirror-registry gcr.io=https://gcr.io
```

**Benefits:** Avoid Docker Hub rate limits, offline development, faster CI/CD

## Secret Management

Encrypt and decrypt secrets using [SOPS](concepts.md#sops) with support for age, PGP, and cloud KMS providers.

```bash
ksail cipher encrypt secret.yaml
ksail cipher decrypt secret.enc.yaml
ksail cipher edit secret.enc.yaml
ksail cipher import AGE-SECRET-KEY-1...
```

**Commands:** [`ksail cipher`](configuration/cli-flags/cipher/cipher-root.md)

| Command                | Description                   |
| ---------------------- | ----------------------------- |
| `ksail cipher encrypt` | Encrypt a file with SOPS      |
| `ksail cipher decrypt` | Decrypt a SOPS-encrypted file |
| `ksail cipher edit`    | Edit encrypted file in-place  |
| `ksail cipher import`  | Import age private key        |

**Supported KMS:** See [Key Management Systems](concepts.md#key-management-systems) for supported providers and documentation links.

## Code Generation

Generate Kubernetes manifests, Helm releases, and Flux/ArgoCD resources using built-in generators.

```bash
ksail workload gen deployment my-app --image=nginx --port=80
ksail workload gen service my-app --port=80
ksail workload gen helmrelease my-chart --source=oci://registry/chart
```

**Capabilities:**

- **Kubernetes resources** – Deployments, services, configmaps, secrets, ingress, and more
- **[Helm](concepts.md#helm) releases** – HelmRelease CRs for Flux
- **Source resources** – OCIRepository, GitRepository, HelmRepository

**Commands:** [`ksail workload gen`](configuration/cli-flags/workload/gen/workload-gen-root.md), [`ksail workload create`](configuration/cli-flags/workload/create/workload-create-root.md)

## Declarative Configuration

Define cluster configuration in `ksail.yaml` for reproducible, version-controlled environments.

```yaml
# ksail.yaml
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
    cni: Cilium
    gitOpsEngine: Flux
    localRegistry: Enabled
  workload:
    sourceDirectory: k8s
```

**Benefits:** Team consistency, reproducible environments, Git-tracked configuration

**Reference:** [Declarative Configuration](configuration/declarative-configuration.md)

## Next Steps

- **[Use Cases](use-cases.md)** – Workflows for learning, development, and CI/CD
- **[Concepts](concepts.md)** – Understand the technologies KSail builds upon
- **[Configuration](configuration/index.md)** – Complete configuration reference
