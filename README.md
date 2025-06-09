# KSail

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Test](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml/badge.svg?branch=main)](https://github.com/devantler-tech/ksail/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/devantler-tech/ksail/graph/badge.svg?token=DNEO90PfNR)](https://codecov.io/gh/devantler-tech/ksail)

<picture align="center">
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/ksail-cli-dark.png" style="width: 550px">
  <source media="(prefers-color-scheme: light)" srcset="docs/images/ksail-cli-light.png" style="width: 550px">
  <img alt="KSail CLI" src="docs/images/ksail-cli-dark.png" style="width: 550px">
</picture>

## Getting Started

> [!IMPORTANT]
> Docker Desktop 4.42.0 bugged out KSail on MacOS. There is a temporary fixed Docker Desktop version here: https://github.com/docker/for-mac/issues/7693#issuecomment-2950044483
> Please update Docker Desktop to the latest version, or try the above linked version before posting a bug issue :-)


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
