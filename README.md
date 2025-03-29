# KSail

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Test](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml/badge.svg?branch=main)](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=DNEO90PfNR)](https://codecov.io/gh/devantler-tech/ksail)

![KSail CLI](docs/images/ksail-cli.png)

## Getting Started

### Prerequisites

- MacOS (amd64 and arm64)
- Linux (amd64 and arm64)

### Installation

#### Homebrew

```sh
brew tap devantler-tech/formulas
brew install ksail
```

#### Manually

1. Download the latest release for your OS from the [releases page](https://github.com/devantler-tech/ksail/releases).
2. Make the binary executable: `chmod +x ksail`.
3. Move the binary to a directory in your `$PATH`: `mv ksail /usr/local/bin/ksail`.

### Usage

Getting started with KSail is easy. First, you need to configure what cluster you want KSail to manage.

`> ksail gen config ksail` - To generate a new KSail configuration file.

Now you can make changes to the configuration file to your liking, and when you are ready to initialize a new cluster, you can run:

`> ksail init` - To initialize a new cluster configuration based on your KSail configuration.

From there, you probably want to ensure that your distribution and ksail is configured to your liking.

- `kind.yaml` - The configuration file for Kind.
- `k3d.yaml` - The configuration file for K3d.

When you are ready to create your cluster, you can run:

`> ksail up` - To create your new cluster.

From there, you can make some changes to your manifest files, and when you are ready to apply them, you can run:

`> ksail update` - To update your cluster.

And for more advanced debugging, you can run:

`> ksail debug` - To debug your cluster with the K9s tool.

Finally, when you are done working with your cluster, you can run:

`> ksail stop` - To stop your cluster, so you can continue working on it later.

Or if you really want to get rid of it for now, you can run:

`> ksail down` - To dismantle your cluster and remove its resources.

## Documentation

- [Overview](https://devantler-tech.github.io/ksail/docs/overview)
- [Use Cases](https://devantler-tech.github.io/ksail/docs/use-cases)
- [Configuration](https://devantler-tech.github.io/ksail/docs/configuration)
- [FAQ](https://devantler-tech.github.io/ksail/docs/faq)
- [Roadmap](https://devantler-tech.github.io/ksail/docs/roadmap)

## Sub-projects

KSail is part of a larger ecosystem of projects that I maintain. These projects are designed to provide important functionality to KSail with high confidence, and without bloating the main repository. Most the projects provide extensible frameworks, so you can build your own tools on top of them, or make contributions to make KSail even more powerful.

- **[devantler-tech/dotnet-container-engine-provisioner](https://github.com/devantler-tech/dotnet-container-engine-provisioner)** - Provisioners to provision resources in container engines like Docker or Podman.
- **[devantler-tech/dotnet-keys](https://github.com/devantler-tech/dotnet-keys)** - A library with key models, like the Age key model.
- **[devantler-tech/dotnet-kubernetes-generator](https://github.com/devantler-tech/dotnet-kubernetes-generator)** - Generators to create Kubernetes resources with `ksail init` and `ksail gen`.
- **[devantler-tech/dotnet-kubernetes-provisioner](https://github.com/devantler-tech/dotnet-kubernetes-provisioner)** - Provisioners to provision Kubernetes clusters and resources.
- **[devantler-tech/dotnet-kubernetes-validator](https://github.com/devantler-tech/dotnet-kubernetes-validator)** - A library that validates Kubernetes resources client-side or at runtime.
- **[devantler-tech/dotnet-secret-manager](https://github.com/devantler-tech/dotnet-secret-manager)** - A secret manager to manage secrets in Git in a secure way.
- **[devantler-tech/dotnet-template-engine](https://github.com/devantler-tech/dotnet-template-engine)** - A template engine to support code generation of non-serializable content.

### CLI Wrappers

These projects are CLI wrappers that provide a C# API for the respective CLI tools. They are used in KSail to embed and interact with CLI tools in a type-safe manner.

- **[devantler-tech/dotnet-age-cli](https://github.com/devantler-tech/dotnet-age-cli)** - A library that embeds and provides an API for the Age CLI.
- **[devantler-tech/dotnet-cilium-cli](https://github.com/devantler-tech/dotnet-cilium-cli)** - A library that embeds and provides an API for the Cilium CLI.
- **[devantler-tech/dotnet-flux-cli](https://github.com/devantler-tech/dotnet-flux-cli)** - A library that embeds and provides an API for the Flux CLI.
- **[devantler-tech/dotnet-helm-cli](https://github.com/devantler-tech/dotnet-helm-cli)** - A library that embeds and provides an API for the Helm CLI.
- **[devantler-tech/dotnet-k3d-cli](https://github.com/devantler-tech/dotnet-k3d-cli)** - A library that embeds and provides an API for the K3d CLI.
- **[devantler-tech/dotnet-k9s-cli](https://github.com/devantler-tech/dotnet-k9s-cli)** - A library that embeds and provides an API for the K9s CLI.
- **[devantler-tech/dotnet-kind-cli](https://github.com/devantler-tech/dotnet-kind-cli)** - A library that embeds and provides an API for the Kind CLI.
- **[devantler-tech/dotnet-kubeconform-cli](https://github.com/devantler-tech/dotnet-kubeconform-cli)** - A library that embeds and provides an API for the Kubeconform CLI.
- **[devantler-tech/dotnet-kubectl-cli](https://github.com/devantler-tech/dotnet-kubectl-cli)** - A library that embeds and provides an API for the Kubectl CLI.
- **[devantler-tech/dotnet-kustomize-cli](https://github.com/devantler-tech/dotnet-kustomize-cli)** - A library that embeds and provides an API for the Kustomize CLI.
- **[devantler-tech/dotnet-sops-cli](https://github.com/devantler-tech/dotnet-sops-cli)** - A library that embeds and provides an API for the SOPS CLI.

## Related Projects

KSail is a powerful tool that can be used in many different ways. Here are some projects that use KSail in their setup:

- **[devantler/homelab](https://github.com/devantler/homelab)** - My personal homelab setup.

If you use KSail in your project, feel free to open a PR to add it to the list, so others can see how you use KSail.

## Presentations

- **[KSail - a Kubernetes SDK for local GitOps development and CI](https://youtu.be/Q-Hfn_-B7p8?si=2Uec_kld--fNw3gm)** - A presentation on KSail at KCD2024.

## Star History

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date&theme=dark"/>
  <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
  <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
</picture>
