[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Reference](https://pkg.go.dev/badge/github.com/devantler-tech/ksail.svg)](https://pkg.go.dev/github.com/devantler-tech/ksail)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=HSUfhaiXwq)](https://codecov.io/gh/devantler-tech/ksail)
[![CI - Go](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml/badge.svg)](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml)

# KSail

KSail is a CLI tool for creating and maintaining local Kubernetes clusters. It provides a unified interface for managing clusters and workloads across different distributions (currently Kind and K3d, with more planned). By wrapping existing tools with a consistent command-line experience, KSail eliminates the complexity of juggling multiple CLIs.

KSail simplifies your Kubernetes workflow by providing:

- 🎯 A single command-line interface for Kind and K3d clusters
- 📝 Declarative configuration for reproducible environments
- 🔐 Integrated workload and secrets management
- ⚡ Fast cluster lifecycle operations (create, start, stop, delete)

Whether you're developing applications, testing infrastructure changes, or learning Kubernetes, KSail gets you from zero to a working cluster in seconds.

🌟 Declarative. Local. Effortless. Welcome to Kubernetes, simplified.

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

| Purpose                                                    | Command Example            |
|------------------------------------------------------------|----------------------------|
| Initialize a new KSail cluster project                     | `ksail cluster init`       |
| Create and start the cluster                               | `ksail cluster create`     |
| Create a workload in the cluster                           | `ksail workload create`    |
| Apply workloads to the cluster                             | `ksail workload apply`     |
| Reconcile workloads (requires configuring a GitOps engine) | `ksail workload reconcile` |
| Connect to the cluster                                     | `ksail cluster connect`    |
| Stop the cluster                                           | `ksail cluster stop`       |
| Delete the cluster                                         | `ksail cluster delete`     |

This is just a small sample of what KSail can do. For a full list of commands and options, run `ksail --help` or refer to the [documentation](#documentation).

## Documentation

### For users

- Browse the documentation in [`docs/`](./docs/) (Markdown) or on <https://ksail.devantler.tech> (GitHub Pages).

### For contributors

- [CONTRIBUTING.md](./CONTRIBUTING.md) — Contribution guide, development prerequisites, and workflows
- [API Documentation](https://pkg.go.dev/github.com/devantler-tech/ksail) — Go package documentation

## Related Projects

KSail is a powerful tool that can be used in many different ways. Here are some projects that use KSail in their setup:

| Project                                                               | Description         | Type     |
|-----------------------------------------------------------------------|---------------------|----------|
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
