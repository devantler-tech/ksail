---
parent: Overview
nav_order: 2
---

# Support Matrix

KSail focuses on fast local Kubernetes development. The matrix below captures officially supported features. Items marked ✅ are fully implemented and tested.

| Category                           | Supported Options                                  | Notes                                                                                                     |
|------------------------------------|----------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| CLI Platforms                      | Linux (amd64, arm64), macOS (amd64, arm64)         | Pre-built binaries available; Windows support tracked separately.                                        |
| Container Engines                  | Docker ✅                                           | Podman support planned for future release.                                                                |
| Distributions                      | Kind ✅, K3d ✅                                      | Both distributions fully supported.                                                                       |
| Workload Management                | kubectl ✅, Helm ✅                                  | Commands wrapped via `ksail workload`.                                                                    |
| GitOps Engines                     | ArgoCD ✅ (Flux planned)                            | ArgoCD integration available; Flux integration in development.                                           |
| Container Network Interfaces (CNI) | Default ✅, Cilium ✅, None ✅                        | Choose via `spec.cni` or `--cni` flag.                                                                    |
| Container Storage Interfaces (CSI) | Default, LocalPathStorage (not yet implemented)    | Configuration defined but not fully implemented.                                                          |
| Metrics Server                     | Enabled ✅, Disabled ✅                              | Toggle with `--metrics-server` during init or create.                                                     |
| Cert-Manager                       | Enabled ✅, Disabled ✅                              | Toggle with `--cert-manager` during init or create.                                                       |
| Local Registry                     | Enabled ✅, Disabled ✅                              | OCI registry for local image storage and GitOps scenarios.                                                |
| Mirror Registries                  | Supported ✅                                        | Configure with `--mirror-registry` flags.                                                                 |
| Secret Management                  | SOPS via `ksail cipher` ✅                          | Encrypt/decrypt files with SOPS; GitOps integration planned.                                              |
| Ingress Controllers                | Planned (use distribution defaults for now)        | Configure through `kind.yaml` or `k3d.yaml`.                                                              |
| Gateway Controllers                | Planned                                            | Gateway API support in development.                                                                       |

> **Note:** This support matrix reflects the Go rewrite of KSail. Features such as Podman support, GitOps engines (Flux and ArgoCD), advanced CSI backends, and Ingress/Gateway controllers are in active development or being reimplemented from the previous .NET version. For up‑to‑date details and timelines, see the KSail roadmap and issues at https://github.com/devantler-tech/ksail/issues.
