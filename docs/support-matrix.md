---
title: "Support Matrix"
nav_order: 5
---

# Support Matrix

| Category            | Supported Options                                           | Status/Notes                                                |
| ------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| Platforms           | Linux (amd64, arm64), macOS (arm64), Windows (amd64, arm64) | ⚠️ Windows support is untested.                             |
| Distributions       | Kind, K3d, Talos                                            | ✅ All three distributions fully supported.                 |
| Workload Management | kubectl, Helm                                               | ✅ Commands wrapped via `ksail workload`.                   |
| GitOps Engines      | Flux, ArgoCD                                                | ✅ Both engines fully supported.                            |
| CNI                 | Default, Cilium, Calico                                     | ✅ Choose via `spec.cluster.cni` or `--cni` flag.           |
| CSI                 | Default, LocalPathStorage                                   | ✅ Choose via `spec.cluster.csi` or `--csi` flag.           |
| Metrics Server      | Default, Enabled, Disabled                                  | ✅ Toggle with `--metrics-server` flag.                     |
| Cert-Manager        | Enabled, Disabled                                           | ✅ Toggle with `--cert-manager` flag.                       |
| Local Registry      | Enabled, Disabled                                           | ✅ OCI registry for local image storage.                    |
| Mirror Registries   | Configurable                                                | ✅ Configure with `--mirror-registry` flags.                |
| Secret Management   | SOPS via `ksail cipher`                                     | ✅ Encrypt/decrypt files with age, PGP, and cloud KMS keys. |

> **Note:** Features marked with ⚠️ are in active development or being reimplemented. For up‑to‑date details, see the KSail [roadmap and issues](https://github.com/devantler-tech/ksail/issues).
