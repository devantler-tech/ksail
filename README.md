---
title: Home
nav_order: 0
---

# KSail

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Test](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml/badge.svg?branch=main)](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=DNEO90PfNR)](https://codecov.io/gh/devantler-tech/ksail)

> [!IMPORTANT]
> **🆕 UP NEXT 🆕**
>
> 1. [KSail as Go project](https://github.com/devantler-tech/ksail/pull/1352) (same cli <-> total rework)
> 2. Support for ArgoCD as a Deployment Tool - <https://github.com/devantler-tech/ksail/pull/878>
> 3. Support for Talos Linux as a Distribution

<picture align="center">
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/ksail-cli-dark.png" style="width: 550px">
  <source media="(prefers-color-scheme: light)" srcset="docs/images/ksail-cli-light.png" style="width: 550px">
  <img alt="KSail CLI" src="docs/images/ksail-cli-dark.png" style="width: 550px">
</picture>

Take control of Kubernetes without the chaos. ⚡ **KSail** is your all-in-one SDK for spinning up clusters and managing workloads—right from your own machine. Instead of juggling a dozen CLI tools, KSail streamlines your workflow with a single, declarative interface built on top of the Kubernetes tools you already know and trust.

🌟 Declarative. Local. Effortless. Welcome to Kubernetes, simplified.

## Getting Started

### Prerequisites

- Linux (amd64 and arm64)
- MacOS (amd64 and arm64)
- Windows (amd64 and arm64)
  - I am unable to test Windows builds, so please report any issues you encounter.

### Installation

Currently, KSail is available in two ways: via Homebrew or GitHub releases.

#### Homebrew

It is recommended to install KSail using [Homebrew](https://brew.sh) for easy updates and management. If you don't have Homebrew installed, you can find installation instructions on their [website](https://brew.sh).

```sh
brew tap devantler-tech/formulas
brew install ksail
```

#### Manually

> [!WARNING]
> If you install KSail manually, you need to ensure the dependent binaries are available in your `$PATH` for all functionality to work. These include: [age](https://github.com/FiloSottile/age#installation), [argocd](https://argo-cd.readthedocs.io/en/stable/getting_started/#2-download-argo-cd-cli), [cilium](https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli), [flux](https://fluxcd.io/flux/installation/#install-the-flux-cli), [helm](https://helm.sh/docs/intro/install/), [k3d](https://k3d.io/stable/#installation), [k9s](https://k9scli.io/topics/install/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), [kubeconform](https://github.com/yannh/kubeconform?tab=readme-ov-file#installation), [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl), [kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize/binaries/), [sops](https://github.com/getsops/sops/releases), [talosctl](https://www.talos.dev/latest/talos-guides/install/talosctl/)

1. Download the latest release for your OS from the [releases page](https://github.com/devantler-tech/ksail/releases).
2. Make the binary executable: `chmod +x ksail`.
3. Move the binary to a directory in your `$PATH`: `mv ksail /usr/local/bin/ksail`.

### Usage

Getting started with KSail is straightforward. Begin by initializing a new KSail project:

```sh
> ksail init # to create a new default project

> ksail init \ # to create a new custom project (★ is default)
  --container-engine <★Docker★|Podman> \ # the container engine to provision your cluster in
  --distribution <★Kind★|K3d> \ # the kubernetes distribution for your cluster
  --deployment-tool <★Kubectl★|Flux> \ # the tool you want to use for declarative deployments
  --cni <★Default★|Cilium|None> \ # the Container Network Interface (CNI) you want pre-installed
  --csi <★Default★|LocalPathProvisioner|None> \ # the Container Storage Interface (CSI) you want pre-installed
  --ingress-controller <★Default★|Traefik|None> \ # the Ingress Controller you want pre-installed
  --gateway-controller <★Default★|None> \ # the Gateway Controller you want pre-installed
  --metrics-server <★True★|False> \ # whether metrics server should be pre-installed
  --secret-manager <★None★|SOPS> \ # the secret manager you want to use to manage secrets in Git
  --mirror-registries <★True★|False> \ # whether mirror registries should be set up or not
  --editor <★Nano★|Vim> # the editor you want to use for commands that require it
```

This creates the following project files, depending on your choices:

```sh
├── ksail.yaml # Configuration for KSail
├── <distribution>.yaml # Configuration for a distribution (e.g., kind.yaml, k3d.yaml)
├── .sops.yaml # Configuration for SOPS - the secret manager (if enabled)
└── k8s # Kubernetes manifests
    └── kustomization.yaml # The entry point for your workloads
```

Customize these files to suit your setup. Once ready, create your cluster with:

```sh
> ksail up # to create the cluster
```

You can then modify your manifest files in the `k8s` folder as needed. To apply changes to your cluster, use:

```sh
> ksail update # to apply changes to the cluster
```

For advanced debugging, connect to the cluster via the [K9s](https://k9scli.io) tool with:

```sh
> ksail connect # to connect to the cluster
```

When you're done, you can stop the cluster to resume later:

```sh
> ksail stop # to shut down the cluster

> ksail start # to start the cluster again
```

Or completely remove it and its resources with:

```sh
> ksail down # to dismantle the cluster and all of its resources
```

For more details on the available commands, checkout the [KSail CLI Options](https://ksail.devantler.tech/docs/configuration/cli-options.html) page.

## Documentation

The documentation for KSail is available at [ksail.devantler.tech](https://ksail.devantler.tech).

## Related Projects

KSail is a powerful tool that can be used in many different ways. Here are some projects that use KSail in their setup:

| Project                                                                       | Description                                                                | Type             |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------------- | ---------------- |
| [devantler-tech/platform](https://github.com/devantler-tech/platform)         | A platform I use for personal projects.                                    | Platform         |
| [devantler-tech/testkube-poc](https://github.com/devantler-tech/testkube-poc) | A proof of concept for using TestKube as a test framework in your cluster. | Proof of Concept |
| [devantler-tech/pinniped-poc](https://github.com/devantler-tech/pinniped-poc) | A proof of concept for using Pinniped for authenticating to your cluster.  | Proof of Concept |

If you use KSail in your project, feel free to open a PR to add it to the list, so others can see how you use KSail.

## Presentations

- **[KSail - a Kubernetes SDK for local GitOps development and CI](https://youtu.be/Q-Hfn_-B7p8?si=2Uec_kld--fNw3gm)** - A presentation on KSail at KCD2024.

## Star History

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date&theme=dark"/>
  <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
  <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=devantler-tech/ksail&type=Date"/>
</picture>
