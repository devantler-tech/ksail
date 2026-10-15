<!-- mcp-name: io.github.devantler-tech/ksail -->

[![GitHub Stars](https://img.shields.io/github/stars/devantler-tech/ksail?style=flat)](https://github.com/devantler-tech/ksail/stargazers)
[![Latest Release](https://img.shields.io/github/v/release/devantler-tech/ksail)](https://github.com/devantler-tech/ksail/releases/latest)
[![License](https://img.shields.io/badge/License-PolyForm--Shield--1.0.0-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/devantler-tech/ksail/v7.svg)](https://pkg.go.dev/github.com/devantler-tech/ksail/v7)
[![CI - KSail](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml/badge.svg)](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml)

# 🛥️🐳 KSail

![KSail command-line interface](docs/src/assets/ksail-cli-dark.png)

KSail brings Kubernetes cluster setup, application deployment and everyday operations into one tool.
Create local clusters with Docker, or work with supported cloud providers and existing Kubernetes
clusters. Use it from your terminal, VS Code or an AI assistant.

📖 **[Documentation](https://ksail.devantler.tech)** · **[Quickstart](https://ksail.devantler.tech/start/quickstart/)** · **[Support](#community--support)**

## What you get

| Task | Included tools and capabilities |
|------|---------------------------------|
| Create clusters | Kind, K3d, Talos, vCluster, KWOK, EKS, GKE and AKS |
| Deploy applications | kubectl, Helm, Kustomize, Flux and ArgoCD |
| Protect configuration | Encrypt secrets with SOPS and Age; validate manifests with Kubeconform |
| Operate clusters | K9s, backup and restore, multi-tenancy |
| Work with AI | Chat assistant, MCP server and VS Code extension |

See [features](https://ksail.devantler.tech/features/) and the
[support matrix](https://ksail.devantler.tech/support-matrix/) for supported platforms and combinations.
KWOK simulates clusters; it does not run applications. Cloud providers need their own setup and incur charges.

## Quick install

On Linux or an Apple Silicon Mac, with `curl`, `tar` and `sha256sum` or `shasum` available:

```sh
curl -fsSL https://github.com/devantler-tech/ksail/releases/latest/download/install.sh | sh
```

The installer verifies the download and prints where it installed KSail. Follow its `PATH`
instruction if needed. Prefer Homebrew or a Windows download? See
[installation options](docs/reader-reference.md#installation-options).

## Quick start

Create a project in a new directory:

```sh
mkdir my-app
cd my-app
ksail project init --name my-app --distribution Vanilla --provider Docker
```

This creates your project files without needing Docker or a cloud account.
With Docker Engine or Docker Desktop running, start and explore a local cluster:

```sh
ksail cluster create
ksail cluster connect
```

This creates a local Kubernetes cluster and opens K9s to explore it. Enter `:quit` to leave K9s.
When finished, delete the trial cluster and its applications:

```sh
ksail cluster delete --name my-app --provider Docker
```

Review the cluster name and confirm deletion when prompted. Project files remain for reuse.

Continue with the [quickstart guide](https://ksail.devantler.tech/start/quickstart/) to deploy an application.
The [reader reference](docs/reader-reference.md) covers local setup details, cloud providers and optional
[AI assistant plugins](docs/reader-reference.md#ai-assistant-plugins).

## Community & support

- 💬 [Discussions](https://github.com/devantler-tech/ksail/discussions) — questions and ideas.
- 🐛 [Issues](https://github.com/devantler-tech/ksail/issues) — bugs and feature requests.
- 🎓 [Resources](https://ksail.devantler.tech/resources/) — presentations and tutorials.
- 🛠️ [Contributing](CONTRIBUTING.md) — make your first contribution.

## License

KSail is source-available under [PolyForm Shield 1.0.0](LICENSE), with use, redistribution and
noncompetition terms. See the [licensing guide](docs/reader-reference.md#license) and full license
before reusing it.
