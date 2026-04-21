<!-- mcp-name: io.github.devantler-tech/ksail -->
[![GitHub Stars](https://img.shields.io/github/stars/devantler-tech/ksail?style=flat)](https://github.com/devantler-tech/ksail/stargazers)
[![Latest Release](https://img.shields.io/github/v/release/devantler-tech/ksail)](https://github.com/devantler-tech/ksail/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/devantler-tech/ksail/v7)](https://goreportcard.com/report/github.com/devantler-tech/ksail/v7)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/license/apache-2-0)
[![Go Reference](https://pkg.go.dev/badge/github.com/devantler-tech/ksail/v7.svg)](https://pkg.go.dev/github.com/devantler-tech/ksail/v7)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=HSUfhaiXwq)](https://app.codecov.io/gh/devantler-tech/ksail)
[![CI - KSail](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml/badge.svg)](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml)
[![MCP Registry](https://img.shields.io/badge/MCP_Registry-io.github.devantler--tech/ksail-blue?logo=github)](https://github.com/mcp)

# 🛥️🐳 KSail

![ksail](./docs/src/assets/ksail-cli-dark.png)

KSail bundles common Kubernetes tooling into a single binary. Spin up local clusters, deploy workloads, and operate cloud-native stacks across distributions and providers through a CLI, VS Code extension, AI chat TUI, or MCP server — with **only Docker required**.

📖 **Full documentation:** <https://ksail.devantler.tech>

## Quick Install

```bash
# macOS / Linux (Homebrew)
brew install --cask devantler-tech/tap/ksail

# Go (1.26.1+)
go install github.com/devantler-tech/ksail/v7@latest
```

See the [Installation Guide](https://ksail.devantler.tech/installation/) for binary downloads and more options.

## AI Assistant Plugins

Install the ksail plugin for [GitHub Copilot CLI](https://docs.github.com/en/copilot/how-tos/copilot-cli) or [Claude Code](https://docs.claude.com/en/docs/claude-code/plugins) to auto-register ksail's MCP server and a ksail expertise skill.

**Copilot CLI:**

```bash
copilot plugin marketplace add devantler-tech/ksail
copilot plugin install ksail
```

**Claude Code:**

```text
/plugin marketplace add devantler-tech/ksail
/plugin install ksail@ksail
```

Requires `ksail` on `PATH`.

## Quick Start

```bash
ksail cluster init --name my-app   # scaffold project + native configs
ksail cluster create               # spin up the cluster (Docker only)
ksail cluster connect              # open K9s
```

Continue with the [Getting Started guide](https://ksail.devantler.tech/) for GitOps, workloads, and multi-tenancy.

## What KSail Bundles

| Category                 | Built-in Capabilities                                       |
|--------------------------|-------------------------------------------------------------|
| Cluster Provisioning     | Kind, K3d, Talos, VCluster (Vind), KWOK (kwokctl), EKS      |
| Container Orchestration  | kubectl, Helm, Kustomize                                    |
| GitOps Engines           | Flux, ArgoCD                                                |
| Secrets Management       | SOPS with Age encryption                                    |
| Manifest Validation      | Kubeconform                                                 |
| Cluster Operations       | K9s, backup & restore, multi-tenancy (`ksail tenant`)       |
| AI Integration           | Chat assistant (Copilot SDK), MCP server, VS Code extension |
| Infrastructure Providers | Docker (local), Hetzner Cloud, Sidero Omni, AWS             |

See the [feature overview](https://ksail.devantler.tech/features/) and [architecture guide](https://ksail.devantler.tech/architecture/) for details.

## Supported Platforms

| OS                                            | Architecture |
|-----------------------------------------------|--------------|
| 🐧 Linux                                      | amd64, arm64 |
| 🍎 macOS                                      | arm64        |
| ⊞ Windows (native untested; WSL2 recommended) | amd64, arm64 |

| Provider | Vanilla  | K3s     | Talos | VCluster | KWOK        | EKS |
|----------|----------|---------|-------|----------|-------------|-----|
| Docker   | ✅ (Kind) | ✅ (K3d) | ✅     | ✅ (Vind) | ✅ (kwokctl) | —   |
| Hetzner  | —        | —       | ✅     | —        | —           | —   |
| Omni     | —        | —       | ✅     | —        | —           | —   |
| AWS      | —        | —       | —     | —        | —           | 🚧  |

## Community & Support

- 💬 **[GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)** — questions, ideas, and community
- 🐛 **[Issue Tracker](https://github.com/devantler-tech/ksail/issues)** — bugs and feature requests
- 📖 **[Documentation](https://ksail.devantler.tech)** — guides, CLI reference, architecture
- 🎓 **[Resources](https://ksail.devantler.tech/resources/)** — presentations, blog posts, tutorials
- ⭐ **[Star the repo](https://github.com/devantler-tech/ksail)** — help others discover KSail

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for the development process, coding standards, and PR guidelines. Start with issues labeled [`good first issue`](https://github.com/devantler-tech/ksail/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

## Related Projects

| Project                                                               | Description         | Type     |
|-----------------------------------------------------------------------|---------------------|----------|
| [devantler-tech/platform](https://github.com/devantler-tech/platform) | My personal homelab | Platform |

Using KSail in your project? Open a PR to add it here.

## Star History

<a href="https://www.star-history.com/#devantler-tech/ksail&type=timeline&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=timeline&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=timeline&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=timeline&legend=top-left" />
 </picture>
</a>
