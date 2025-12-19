---
title: "Project Structure"
parent: Overview
nav_order: 1
---

# Project Structure

Running `ksail cluster init` scaffolds a project with the necessary configuration files. The layout varies based on flags like `--distribution` and options in `ksail.yaml`, but every project starts with:

```text
├── ksail.yaml              # Declarative cluster configuration
├── kind.yaml / k3d.yaml    # Distribution-specific configuration
└── k8s/                    # Workload manifests directory
    └── kustomization.yaml  # Root Kustomize entrypoint
```

## Organizing with Kustomize

KSail uses [Kustomize](https://kustomize.io/) for manifest management. The `k8s/kustomization.yaml` file serves as the entry point for workload commands:

- **Structure** – Organize manifests using Kustomize bases and overlays
- **Validation** – Test changes locally with `ksail workload apply`
- **GitOps ready** – When GitOps support is added, the same manifests will be used

Use `--source-directory` during init to change where workloads are stored (default: `k8s`).

## Configuration Files

### `ksail.yaml`

The main configuration file defining your cluster setup. See [Declarative Config](../configuration/declarative-config.html) for details.

### Distribution configs

- **`kind.yaml`** – [Kind configuration](https://kind.sigs.k8s.io/docs/user/configuration/) for node layout, networking, and port mappings
- **`k3d.yaml`** – [K3d configuration](https://k3d.io/stable/usage/configfile/) for K3s-specific options

Choose which file to use with the `spec.distributionConfig` field in `ksail.yaml`.

## Adding Workloads

Place Kubernetes manifests in the `k8s/` directory (or your configured `sourceDirectory`). Use standard Kustomize structure:

```text
k8s/
├── kustomization.yaml
├── namespace.yaml
└── apps/
    ├── kustomization.yaml
    └── deployment.yaml
```

Apply workloads with:

```bash
ksail workload apply -k k8s/
```
