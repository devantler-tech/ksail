---
title: Container Network Interfaces (CNIs)
parent: Core Concepts
nav_order: 2
---

# Container Network Interfaces (CNIs)

## Default

The `Default` CNI is the default Container Network Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CNI plugin with limited features.

Below is a table of the default CNI plugins for each Kubernetes distribution supported by KSail:

| Distribution | CNI                                                                           |
| ------------ | ----------------------------------------------------------------------------- |
| Kind         | [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd) |
| K3d          | [flannel](https://github.com/flannel-io/flannel)                              |

## Cilium

[Cilium](https://cilium.io/) is a powerful CNI plugin that provides advanced networking and security features for Kubernetes clusters. It uses eBPF (Extended Berkeley Packet Filter) technology to provide high-performance networking, load balancing, and security policies.

Using the [Cilium](https://cilium.io/) CNI will create a Kubernetes cluster with the Cilium CNI plugin pre-installed.

## None

The `None` CNI option means that no Container Network Interface plugin will be installed in your Kubernetes cluster. This is useful if you want to install an unsupported CNI plugin.
