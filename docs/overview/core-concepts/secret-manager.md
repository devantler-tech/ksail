---
title: Secret Manager
parent: Core Concepts
nav_order: 8
---

# Secret Manager

> [!TIP]
> The `Secret Manager` is disabled by default, as it is considered an advanced feature. If you want to use it, you should enable it when initializing the project. This ensures that a new encryption key is created on your system, and that the secret manager is correctly configured with your chosen distribution and deployment tool. If you do not enable it, you will have to manually configure the secret manager later.

KSail uses [`SOPS`](https://getsops.io) as the secret manager. This is a tool that is used to encrypt and decrypt secrets in a way that is compatible with GitOps based deployment tools. It is designed to work with Git and provides a way to keep sensitive values encrypted in Git.

KSail ensures that a private key is securely stored in the cluster, allowing for seamless decryption of secrets when they are applied to the cluster.

> [!NOTE]
> The secret manager `SOPS` supports both `PGP` and `Age` key pairs, but for now, KSail only supports `Age` key pairs. This is because `Age` is a newer and simpler encryption format that is designed to be easy to use and understand. It is also more secure than `PGP` because it solely uses modern cryptography and does not rely on any legacy algorithms or protocols.

## None

No secret manager is used. This means that encryption secrets are not bootstrapped into the cluster, and that encrypted secrets cannot be applied to the cluster. This is the default option.

## SOPS

SOPS is used as the secret manager. This ensures a `.sops.yaml` config file is created in the project root, and that an encryption key is bootstrapped into the cluster, for the deployment tools that support encrypting secrets as part of the deployment process.

> [!IMPORTANT]
> KSail only supports decryption of secrets as part of the deployment process with the `Flux` deployment tool. For other deployment tools, it is still possible to enable, with tooling like [`ksops`](https://github.com/viaduct-ai/kustomize-sops). Setting this up is currently deemed out of scope for KSail. This is a feature that may be added in the future, but as `ksops` is a binary tool that is called by via a `kustomize` plugin, it is not a simple task to implement in KSail. I am interested in using upstream functionality, instead of creating another solution to solve the same problem. As such, I will continue to monitor developments in this area.
