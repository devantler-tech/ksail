---
title: Distributions
parent: Core Concepts
layout: default
nav_order: 1
---

# Distributions

`Distributions` refer to the underlying Kubernetes distribution that is used to create the cluster. This can be the providers native distribution, or some other distribution that is compatible with the provider. The `Distribution` is responsible for providing the Kubernetes API and the underlying components that are used to run the cluster.

## Native

The `Native` distribution is the default Kubernetes distribution provided by a specific `Provider`. In most cases this is a distribution that is optimized for the provider and is designed to work seamlessly with the provider's infrastructure.

Below is the actual distribution used when using the `Native` distribution with the various engines.

- **Docker**: The `Native` distribution is [`kind`](https://kind.sigs.k8s.io/). This is a wrapper around the official Kubernetes distribution that is designed to run in Docker containers. This was decided as the native distribution because it has official support from a [Kubernetes Special Interest Group (SIG)](https://github.com/kubernetes/community/blob/master/sig-list.md).

## [K3s](https://k3s.io/)

The `K3s` distribution is a lightweight Kubernetes distribution that is designed for resource-constrained environments. Its implementation depends on the `Provider` used.

- **Docker**: The `K3s` distribution is `k3d`. This is a wrapper around `k3s` that runs in Docker containers.
