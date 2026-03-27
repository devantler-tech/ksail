[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/license/apache-2-0)
[![Go Reference](https://pkg.go.dev/badge/github.com/devantler-tech/ksail/v5.svg)](https://pkg.go.dev/github.com/devantler-tech/ksail/v5)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=HSUfhaiXwq)](https://app.codecov.io/gh/devantler-tech/ksail)
[![CI - KSail](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml/badge.svg)](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml)

# 🛥️🐳 KSail

![ksail](./docs/src/assets/ksail-cli-dark.png)

KSail is a tool that bundles common Kubernetes tooling into a single binary. It provides a VSCode Extension, CLI, AI-Enabled Chat TUI or MCP interface to create clusters, deploy workloads, and operate cloud-native stacks across different distributions and providers.

## Why KSail?

Setting up and operating Kubernetes clusters often requires juggling multiple CLI tools, writing bespoke scripts, and dealing with inconsistent workflows. KSail removes the tooling overhead so you can focus on your workloads.

## Key Features

- 📦 **One Binary** — Embeds cluster provisioning, GitOps engines, and deployment tooling. No tool sprawl.
- ☸️ **Simple Clusters** — Spin up Vanilla, K3s, Talos, or VCluster clusters with one command. Same workflow across distributions.
- 🔓 **No Lock-In** — Uses native configs (`kind.yaml`, `k3d.yaml`, Talos patches, `vcluster.yaml`). Run clusters with or without KSail.
- 📥 **Mirror Registries** — Avoid rate limits, and store images once. Same mirrors used by different clusters.
- 📄 **Everything as Code** — Cluster settings, distribution configs, and workloads in version-controlled files.
- 🔄 **GitOps Native** — Built-in Flux or ArgoCD support with bootstrap, push, and reconcile commands.
- ⚙️ **Customizable Stack** — Select your CNI, CSI, policy engine, cert-manager, and mirror registries.
- 🔐 **SOPS Built In** — Encrypt, decrypt, and edit secrets with integrated cipher commands.
- 💾 **Backup & Restore** — Export cluster resources to a compressed archive and restore to any cluster with provenance labels.
- 🤖 **AI Assistant** — Interactive chat powered by GitHub Copilot for configuration and troubleshooting.
- 💻 **VSCode Extension** — Manage clusters from VSCode via VS Code Kubernetes extension integration (Cloud Explorer, Cluster Explorer), wizards, and command palette.

## Getting Started

### Installation

The quickest way to install KSail is via our install script:

```bash
curl -sSfL https://raw.githubusercontent.com/devantler-tech/ksail/main/install.sh | sh
```

Alternatively, Go users can install it directly:

```bash
go install github.com/devantler-tech/ksail@latest
```

### Prerequisites

KSail works on all major operating systems and CPU architectures:

| OS                                            | Architecture |
|-----------------------------------------------|--------------|
| 🐧 Linux                                      | amd64, arm64 |
|  macOS                                       | arm64        |
| ⊞ Windows (native untested; WSL2 recommended) | amd64, arm64 |

Supported distributions run on different infrastructure providers:

| Provider | Vanilla  | K3s     | Talos | VCluster |
|----------|----