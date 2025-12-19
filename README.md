[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Reference](https://pkg.go.dev/badge/github.com/devantler-tech/ksail.svg)](https://pkg.go.dev/github.com/devantler-tech/ksail)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=HSUfhaiXwq)](https://codecov.io/gh/devantler-tech/ksail)
[![CI - Go](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml/badge.svg)](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml)

# 🛥️🐳 KSail

```txt
                    __ ______     _ __
                   / //_/ __/__ _(_) /
                  / ,< _\ \/ _ `/ / /
                 /_/|_/___/\_,_/_/_/
                                   . . .
              __/___                 :
        _____/______|             ___|____     |"\/"|
_______/_____\_______\_____     ,'        `.    \  /
\   -----       -\-\-\-    |    |  ^        \___/  |
~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~
```

KSail is a CLI tool for creating and maintaining local Kubernetes clusters. It provides a unified interface for managing clusters and workloads across different distributions (currently Kind and K3d, with more planned). By wrapping existing tools with a consistent command-line experience, KSail eliminates the complexity of juggling multiple CLIs.

KSail simplifies your Kubernetes workflow by providing:

- 🎯 **Unified CLI** — One interface for cluster and workload management
- 📄 **Declarative Configuration** — Configuration as code for reproducible clusters
- 🔧 **Flexible Configuration** — Configure your cluster with your preferred distribution, CNI, CSI, service mesh and more.
- 🚀 **Fast Setup** — Spin up local clusters in seconds
- ⚡ **GitOps Ready** — Built-in Flux and ArgoCD support for reconciliation via local registry and OCI artifacts
- 🪞 **Mirror Registries** — Cache images locally to avoid rate limits
- 🔐 **Secrets Management** — SOPS integration for encrypting manifests at rest

Whether you're developing applications, testing infrastructure changes, or learning Kubernetes, KSail gets you from zero to a working cluster in seconds.

## Getting Started

### Prerequisites

- 🐧 Linux (amd64 and arm64)
-  MacOS (arm64)
- ⊞ Windows (amd64 and arm64)
- 🐳 Docker

### Installation

#### Homebrew

```bash
brew install devantler-tech/formulas/ksail
```

#### Go install

```bash
go install github.com/devantler-tech/ksail@latest
```

#### From source

```bash
git clone https://github.com/devantler-tech/ksail.git
cd ksail
go build -o ksail
```

## Usage

### Quick Start

Get up and running with a local Kubernetes cluster in three steps:

```bash
# 1. Initialize a new project with your preferred stack
ksail cluster init --distribution Kind --cni Cilium

# 2. Create and start the cluster
ksail cluster create

# 3. Deploy your workloads
ksail workload apply -k ./k8s
```

### Development Workflow

KSail organizes commands around your development lifecycle:

**Cluster Lifecycle**

- `ksail cluster init` — Scaffold a new project with declarative configuration
- `ksail cluster create` — Provision a new cluster (Kind or K3d)
- `ksail cluster start` — Resume a stopped cluster
- `ksail cluster stop` — Pause a running cluster without losing state
- `ksail cluster connect` — Open k9s for interactive debugging
- `ksail cluster delete` — Clean up resources

**Workload Management**

- `ksail workload apply` — Deploy manifests with kubectl or Kustomize
- `ksail workload reconcile` — Trigger GitOps reconciliation (Flux or ArgoCD)
- `ksail workload logs` — View logs from running pods
- `ksail workload exec` — Execute commands in running pods
- `ksail workload gen` — Generate resource templates

**Secrets & Security**

- `ksail cipher encrypt` — Encrypt secrets with SOPS
- `ksail cipher decrypt` — Decrypt secrets with SOPS
- `ksail cipher edit` — Edit encrypted files in place

## Documentation

### For users

- Browse the documentation in [`docs/`](./docs/) (Markdown) or on <https://ksail.devantler.tech> (GitHub Pages).

### For contributors

- [CONTRIBUTING.md](./CONTRIBUTING.md) — Contribution guide, development prerequisites, and workflows
- [API Documentation](https://pkg.go.dev/github.com/devantler-tech/ksail) — Go package documentation

## Related Projects

KSail is a powerful tool that can be used in many different ways. Here are some projects that use KSail in their setup:

| Project                                                               | Description         | Type     |
| --------------------------------------------------------------------- | ------------------- | -------- |
| [devantler-tech/platform](https://github.com/devantler-tech/platform) | My personal homelab | Platform |

If you use KSail in your project, feel free to open a PR to add it to the list, so others can see how you use KSail.

## Presentations

- **[KSail - a Kubernetes SDK for local GitOps development and CI](https://youtu.be/Q-Hfn_-B7p8?si=2Uec_kld--fNw3gm)** - A presentation on KSail at KCD2024 (Early version of KSail).

## Star History ⭐

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date&theme=dark"/>
  <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
  <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
</picture>
