---
title: Container Network Interfaces (CNIs)
parent: Core Concepts
layout: default
nav_order: 2
---

# Container Network Interfaces (CNIs)

## None

> [!WARNING]
> This option is not supported yet.

## Default

The `Default` CNI is the default Container Network Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CNI plugin with limited features.

Because the `Native` distribution varies depending on the `Provider`, the CNI is determined by the `Provider` and `Distribution` you are using.

| Provider | Distribution  | Default CNI                                                                   |
| -------- | ------------- | ----------------------------------------------------------------------------- |
| Docker   | Native (Kind) | [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd) |
| Docker   | K3s (K3d)     | [flannel](https://github.com/flannel-io/flannel)                              |
| Podman   | Native (Kind) | [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd) |
| Podman   | K3s (K3d)     | [flannel](https://github.com/flannel-io/flannel)                              |

## Cilium

Using the [Cilium](https://cilium.io/) CNI will create a Kubernetes cluster with the Cilium CNI plugin installed. It works with all combinations of `Provider` and `Distribution` that are supported by KSail.
