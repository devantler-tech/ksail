---
title: "Support Matrix"
nav_order: 7
---

# Support Matrix

Overview of KSail's supported platforms, distributions, and components.
See [Concepts](concepts.md) for detailed information about each technology,
or [Configuration](configuration/index.md) for setup instructions.

## Platforms and Distributions

| Platform | Architectures | Status       |
| -------- | ------------- | ------------ |
| Linux    | amd64, arm64  | ✅ Supported |
| macOS    | arm64         | ✅ Supported |
| Windows  | amd64, arm64  | ⚠️ Untested  |

| Distribution | Providers | Status       |
| ------------ | --------- | ------------ |
| Kind         | Docker    | ✅ Supported |
| K3d          | Docker    | ✅ Supported |
| Talos        | Docker    | ✅ Supported |

## Cluster Components

| Component            | Options                    | Configuration           |
| -------------------- | -------------------------- | ----------------------- |
| CNI                  | Default, Cilium, Calico    | `--cni` flag            |
| CSI                  | Default, LocalPathStorage  | `--csi` flag            |
| Metrics Server       | Default, Enabled, Disabled | `--metrics-server` flag |
| Kubelet CSR Approver | Auto-installed             | With metrics-server     |
| cert-manager         | Enabled, Disabled          | `--cert-manager` flag   |
| Policy Engine        | None, Kyverno, Gatekeeper  | `--policy-engine` flag  |

## GitOps and Registries

| Component         | Options            | Configuration            |
| ----------------- | ------------------ | ------------------------ |
| GitOps Engine     | None, Flux, ArgoCD | `--gitops-engine` flag   |
| Local Registry    | Enabled, Disabled  | `--local-registry` flag  |
| Mirror Registries | Configurable       | `--mirror-registry` flag |

## Workload Tools

| Tool      | Commands                                   |
| --------- | ------------------------------------------ |
| kubectl   | `apply`, `get`, `logs`, `exec`, `describe` |
| Helm      | `install`                                  |
| Kustomize | `apply -k`                                 |
| SOPS      | `cipher encrypt`, `decrypt`, `edit`        |

> **Note:** Features marked with ⚠️ are untested or in development.
> See the [roadmap](https://github.com/devantler-tech/ksail/issues) for details.
