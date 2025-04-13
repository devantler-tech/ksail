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

- **Docker + Native = Kind**: The `Default` CNI for `kind` is [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd)
- **Docker + K3s = K3d**: The `Default` CNI for `k3d` is [flannel](https://github.com/flannel-io/flannel)

## [Cilium](https://cilium.io/)

Using the `Cilium` CNI will create a Kubernetes cluster with the Cilium CNI plugin installed. It works with all combinations of `Provider` and `Distribution` that are supported by KSail.
