---
title: "Documentation"
nav_order: 1
---

# KSail Documentation

KSail is a CLI tool for managing local Kubernetes clusters and workloads. Built in Go, it provides a unified interface for cluster lifecycle management and wraps common Kubernetes tools behind consistent commands.

![KSail Architecture](images/architecture.drawio.png)

## Who Uses KSail?

Platform engineers, developers, and anyone working with Kubernetes who wants fast feedback loops and consistent tooling across different distributions.

## Command Groups

- **`ksail cluster`** – Scaffold, create, and manage clusters
- **`ksail workload`** – Apply, validate, and manage Kubernetes resources
- **`ksail cipher`** – Encrypt and decrypt secrets with SOPS

## Project Structure

`ksail cluster init` scaffolds:

```text
├── ksail.yaml              # Cluster configuration
├── kind.yaml / k3d.yaml    # Distribution config
└── k8s/                    # Workload manifests
    └── kustomization.yaml
```

- **`ksail.yaml`** – Main configuration (see [Configuration](configuration.md))
- **Distribution configs** – [Kind](https://kind.sigs.k8s.io/docs/user/configuration/) or [K3d](https://k3d.io/stable/usage/configfile/) options
- **`k8s/`** – [Kustomize](https://kustomize.io/) manifests

Use `--source-directory` to change the workload directory (default: `k8s`).

## Support Matrix

KSail focuses on fast local Kubernetes development. The matrix below captures officially supported features.

| Category            | Supported Options                          | Status/Notes                                                      |
|---------------------|--------------------------------------------|-------------------------------------------------------------------|
| CLI Platforms       | Linux (amd64, arm64), macOS (amd64, arm64) | Pre-built binaries available; Windows support tracked separately. |
| Distributions       | Kind, K3d                                  | ✅ Both distributions fully supported.                             |
| Workload Management | kubectl, Helm                              | ✅ Commands wrapped via `ksail workload`.                          |
| GitOps Engines      | Flux, ArgoCD                               | ✅ Both engines fully supported.                                   |
| CNI                 | Default, Cilium, None                      | ✅ Choose via `spec.cni` or `--cni` flag.                          |
| CSI                 | Default, LocalPathStorage                  | ⚠️ Configuration defined but not fully implemented.               |
| Metrics Server      | Enabled, Disabled                          | ✅ Toggle with `--metrics-server` flag.                            |
| Cert-Manager        | Enabled, Disabled                          | ✅ Toggle with `--cert-manager` flag.                              |
| Local Registry      | Enabled, Disabled                          | ✅ OCI registry for local image storage.                           |
| Mirror Registries   | Configurable                               | ✅ Configure with `--mirror-registry` flags.                       |
| Secret Management   | SOPS via `ksail cipher`                    | ✅ Encrypt/decrypt files; GitOps integration planned.              |
| Ingress Controllers | Distribution defaults                      | ⚠️ Configure through `kind.yaml` or `k3d.yaml` for now.           |
| Gateway Controllers | Planned                                    | ⚠️ Gateway API support in development.                            |

> **Note:** Features marked with ⚠️ are in active development or being reimplemented. For up‑to‑date details, see the KSail [roadmap and issues](https://github.com/devantler-tech/ksail/issues).

## Documentation

- **[Configuration](configuration.md)** – Declarative config and CLI options
- **[Use Cases](use-cases.md)** – Workflows for learning, development, and CI/CD
- **[Core Concepts](core-concepts.md)** – CNI, CSI, registries, and components

## Quick Links

- **Installing?** See [github.com/devantler-tech/ksail](https://github.com/devantler-tech/ksail#installation)
- **Issues or questions?** Open an issue or discussion in the repository
