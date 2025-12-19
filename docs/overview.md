---
title: "Overview"
nav_order: 2
---

# Overview

KSail is a CLI tool for managing local Kubernetes clusters and workloads. Built in Go, it provides a unified interface for cluster lifecycle management and wraps common Kubernetes tools behind consistent commands.

![KSail Architecture](images/architecture.drawio.png)

## Who Uses KSail?

KSail is built for platform engineers, developers, and anyone working with Kubernetes who wants a fast feedback loop. The CLI provides a consistent interface across different distributions, making it approachable for engineers learning Kubernetes.

## Key Features

- **Single binary** – One executable with embedded tools; only Docker required
- **Unified CLI** – Consistent commands across Kind and K3d distributions
- **Fast setup** – Spin up clusters in seconds with sensible defaults
- **GitOps ready** – Built-in support for both Flux and ArgoCD with local registry and OCI artifacts
- **Declarative configuration** – Configuration as code for reproducible clusters
- **Flexible components** – Choose your preferred distribution, CNI, CSI, and more
- **Mirror registries** – Cache images locally to avoid rate limits
- **Secrets management** – SOPS integration for encrypting manifests at rest

## What You Can Do with KSail

- **Scaffold projects** – `ksail cluster init` creates a project with configuration files and Kustomize structure
- **Manage clusters** – Use `ksail cluster` subcommands (`create`, `start`, `stop`, `delete`, `info`, `list`, `connect`) to manage clusters
- **Work with manifests** – `ksail workload` commands wrap `kubectl` and Helm for applying and managing workloads
- **Generate resources** – `ksail workload gen` helps create Kubernetes resource manifests
- **Encrypt secrets** – `ksail cipher` wraps SOPS for encrypting and decrypting files

## Project Structure

Running `ksail cluster init` scaffolds a project with the necessary configuration files:

```text
├── ksail.yaml              # Declarative cluster configuration
├── kind.yaml / k3d.yaml    # Distribution-specific configuration
└── k8s/                    # Workload manifests directory
    └── kustomization.yaml  # Root Kustomize entrypoint
```

### Configuration Files

- **`ksail.yaml`** – Main configuration defining cluster setup (see [Configuration](configuration.md))
- **Distribution configs** – [Kind](https://kind.sigs.k8s.io/docs/user/configuration/) or [K3d](https://k3d.io/stable/usage/configfile/) specific options
- **`k8s/`** – [Kustomize](https://kustomize.io/) manifests for workload management

Use `--source-directory` during init to change where workloads are stored (default: `k8s`).

## Support Matrix

KSail focuses on fast local Kubernetes development. The matrix below captures officially supported features.

| Category            | Supported Options                          | Status/Notes                                                      |
|---------------------|--------------------------------------------|-------------------------------------------------------------------|
| CLI Platforms       | Linux (amd64, arm64), macOS (amd64, arm64) | Pre-built binaries available; Windows support tracked separately. |
| Container Engines   | Docker                                     | ✅ Fully supported; Podman support planned.                        |
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
