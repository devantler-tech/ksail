---
title: Container Storage Interfaces (CSIs)
parent: Core Concepts
layout: default
nav_order: 3
---

# Container Storage Interfaces (CSIs)

## None

> [!WARNING]
> This option is not supported yet.

## Default

The `Default` CSI is the default Container Storage Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CSI plugin with limited features.

Because the `Native` distribution varies depending on the `Provider`, the CSI is determined by the `Provider` and `Distribution` you are using.

| Provider | Distribution  | Default CSI                                                                 |
| -------- | ------------- | --------------------------------------------------------------------------- |
| Docker   | Native (Kind) | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
| Docker   | K3s (K3d)     | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
| Podman   | Native (Kind) | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
| Podman   | K3s (K3d)     | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
