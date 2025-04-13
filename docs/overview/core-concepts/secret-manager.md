---
title: Secret Manager
parent: Core Concepts
layout: default
nav_order: 6
---

# Secret Manager

> [!TIP]
> The `Secret Manager` is disabled by default, as it is considered an advanced feature. If you want to use it, you should enable it when initializing the project. This ensures that a new encryption key is created on your system, and that the secret manager is correctly configured with your chosen distribution and deployment tool. If you do not enable it, you will have to manually configure the secret manager later.

KSail uses [`SOPS`](https://getsops.io) as the secret manager. This is a tool that is used to encrypt and decrypt secrets in a way that is compatible with GitOps based deployment tools. It is designed to work with Git and provides a way to keep sensitive values encrypted in Git.

KSail ensures that a private key is securely stored in the cluster, allowing for seamless decryption of secrets when they are applied to the cluster.

> [!NOTE] > `SOPS` supports both `PGP` and `Age` key pairs, but for now, KSail only supports `Age` key pairs. This is because `Age` is a newer and simpler encryption format that is designed to be easy to use and understand. It is also more secure than `PGP` because it solely uses modern cryptography and does not rely on any legacy algorithms or protocols.
