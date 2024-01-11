# ⛴️🐳 KSail

> [!NOTE]
> This is an early release of KSail. I am actively working on the tool, so if you encounter any issues, please let me know 🙏🏻

![image](https://github.com/devantler/ksail/assets/26203420/83a77828-02e1-4d7a-92b7-9e89d0c4e509)

A CLI tool for provisioning GitOps enabled K8s environments in Docker.

## Getting Started

<details>
  <summary>Show/Hide</summary>

<!-- readme-tree start -->
```
.
├── .github
│   └── workflows
├── autocomplete
├── k3d
├── k8s
│   └── clusters
│       └── test
│           ├── flux
│           └── infrastructure
├── scripts
├── src
│   └── KSail
│       ├── CLIWrappers
│       ├── Commands
│       │   ├── Check
│       │   │   └── Handlers
│       │   ├── Down
│       │   │   └── Handlers
│       │   ├── Lint
│       │   │   └── Handlers
│       │   ├── List
│       │   │   └── Handlers
│       │   ├── SOPS
│       │   │   ├── Handlers
│       │   │   └── Options
│       │   ├── Up
│       │   │   ├── Handlers
│       │   │   ├── Options
│       │   │   └── Validators
│       │   └── Update
│       │       └── Handlers
│       ├── Enums
│       ├── Extensions
│       ├── Models
│       │   └── K3d
│       ├── Options
│       ├── Provisioners
│       │   ├── Cluster
│       │   ├── ContainerOrchestrator
│       │   ├── GitOps
│       │   └── SecretManagement
│       └── assets
│           ├── binaries
│           └── k3d
└── tests
    ├── KSail.Tests.Integration
    └── KSail.Tests.Unit

47 directories
```
<!-- readme-tree end -->

</details>

### Prerequisites

- Unix or Linux-based OS.
  - osx-x64 ✅
  - osx-arm64 ✅
  - linux-x64 ✅
  - linux-arm64 ✅
- Docker
- gpg

### Installation

With Homebrew:

```shell
brew tap devantler/formulas
brew install ksail
```

Manually:

1. Download the latest release from the [releases page](https://github.com/devantler/ksail/releases).
2. Make the binary executable: `chmod +x ksail`.
3. Move the binary to a directory in your `$PATH`: `mv ksail /usr/local/bin/ksail`.

### Usage

To get started use the global help flag:

```shell
ksail --help
```

## What is KSail?

KSail is a CLI tool designed to simplify the management of GitOps-enabled Kubernetes clusters in Docker. It provides a set of commands that allow you to easily create, manage, and dismantle Flux-enabled clusters. KSail also integrates with SOPS for managing secrets in Git repositories, and provides features for validating and verifying your clusters.

## How does it work?

KSail leverages several key technologies to provide its functionality:

- **Embedded Binaries:** KSail embeds binaries for tools like k3d, talosctl, and sops, allowing you to use these tools without having to install them separately.
- **Kubernetes-in-Docker Backends:** KSail supports various Kubernetes-in-Docker backends, allowing you to run Kubernetes clusters inside Docker containers.
- **Flux GitOps:** KSail uses Flux to manage the state of your clusters, with your manifest source serving as the single source of truth.
- **Local OCI Registries:** KSail uses local OCI registries to store and distribute Docker images and manifests.
- **SOPS Integration:** KSail integrates with SOPS for managing secrets in Git repositories.
- **Manifest Validation:** KSail validates your manifest files before deploying your clusters.
- **Cluster Reconciliation Verification:** After deploying your clusters, KSail verifies that they reconcile successfully.

## Why was it made?

KSail was created to fill a gap in the tooling landscape for managing GitOps-enabled Kubernetes clusters in Docker. It aims to simplify the process of enabling GitOps, with necessary tools like OCI registries, and SOPS to enable a seamless development environment for K8s.

## Contributing

Contributions to KSail are welcome! You can contribute by reporting bugs, requesting features, or submitting pull requests. When creating an issue or pull request, please provide as much detail as possible to help understand the problem or feature.
```
