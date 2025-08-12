---
title: Distributions
parent: Core Concepts
nav_order: 1
---

# Distributions

`Distributions` refer to the Kubernetes distribution that is running a cluster. The `Distribution` is responsible for providing the Kubernetes API and the underlying components that are used to run the Kubernetes cluster.

## Kind

The [`Kind`](https://kind.sigs.k8s.io/) distribution is a close-to-native Kubernetes distribution that runs on Docker containers. It is built by the official Kubernetes SIG Testing group. `Kind` is the default distribution used by KSail.

`Kind` does not support LoadBalancer service by default, but the [`cloud-provider-kind`](https://github.com/kubernetes-sigs/cloud-provider-kind) project aims to solve this. KSail spins up a container that runs the `cloud-provider-kind` service, which ensures any Kubernetes service of type LoadBalancer is accessible from the host machine. On Windows and MacOS, you can access the services of type LoadBalancer using localhost and the port number that is mapped to the host by the Envoy container `kindccm-*`. It will map a port per service to the host machine.

## K3d

The [`K3d`](https://k3d.io/) distribution is a lightweight Kubernetes distribution that is designed for resource-constrained environments. It is built on top of the [`K3s`](https://k3s.io/) distribution.

`K3d` supports services of type LoadBalancer by default. You need to map the ports for the services of type LoadBalancer to the host machine via the `k3d.yaml` configuration file. This is done by specifying the `ports` field in the `k3d.yaml` file. The ports will be mapped to the host machine when the cluster is created.
