# Support Matrix

KSail-Go focuses on fast local feedback while keeping GitOps compatibility. The matrix below captures the officially supported combinations for the Go CLI. Items marked ✅ are fully tested and verified.

| Category                           | Supported Options                                  | Notes                                                                                                     |
|------------------------------------|----------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| CLI Platforms                      | Linux (amd64, arm64), macOS (amd64, arm64)         | Pre-built binaries ship via GoReleaser; Windows support is tracked separately.                            |
| Container Engines                  | Docker ✅, Podman (preview)                         | Kind and K3d use Docker by default; Podman support is experimental and tracked in follow-up issues.       |
| Distributions                      | Kind ✅, K3d ✅                                      | Additional distributions (Talos, EKS) are planned post-MVP.                                               |
| Deployment Tooling                 | Flux ✅, kubectl ✅                                  | Flux manifests are generated during `cluster init`; kubectl commands are wrapped inside `ksail workload`. |
| Container Network Interfaces (CNI) | Default (Kind), Cilium (via GitOps overlay)        | Choose via `spec.cluster.cni` or `ksail cluster init --cni`.                                              |
| Container Storage Interfaces (CSI) | Default, Local Path Provisioner                    | Configurable through `ksail.yaml`.                                                                        |
| Ingress Controllers                | Default, Traefik                                   | Select with `--ingress-controller` or the declarative config field.                                       |
| Gateway Controllers                | None (default), Experimental                       | Gateway support will return as the Go controllers mature.                                                 |
| Metrics Server                     | Enabled (default), Disabled                        | Toggle with `--metrics-server` during init.                                                               |
| Secret Management                  | SOPS via `ksail cipher` ✅                          | Age keys live alongside `.sops.yaml` when enabled.                                                        |
| Editors for Interactive Flows      | nano, vim (configurable via `spec.project.editor`) | Used by `ksail cipher edit` and other interactive commands.                                               |

If you rely on a combination that is not listed here, please open an issue so we can track validation coverage.
