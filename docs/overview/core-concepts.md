---
title: Core Concepts
parent: Overview
layout: default
nav_order: 0
---

# Core Concepts

Even though KSail is designed to provide a simple and easy to use interface, it is important to understand the core concepts of KSail and related technologies and tooling. This document will take you through the core concepts of KSail, as well as the underlying technologies and tooling that KSail is built on top of.

- [Glossary](#glossary)
- [Providers](#providers)
  - [Docker](#docker)
- [Distributions](#distributions)
  - [Native](#native)
  - [K3s](#k3s)
- [Container Network Interfaces (CNIs)](#container-network-interfaces-cnis)
  - [None](#none)
  - [Default](#default)
  - [Cilium](#cilium)
- [Ingress Controllers](#ingress-controllers)
  - [None](#none-1)
  - [Default](#default-1)
- [Waypoint Controllers](#waypoint-controllers)
  - [None](#none-2)
  - [Default](#default-2)
- [Deployment Tools](#deployment-tools)
  - [None](#none-3)
  - [Kubectl](#kubectl)
  - [Flux](#flux)
  - [ArgoCD](#argocd)
- [Secret Managers](#secret-managers)
  - [None](#none-4)
  - [SOPS](#sops)
- [Local Registry](#local-registry)
- [Mirror Registries](#mirror-registries)

## Glossary

<table>
  <thead>
    <tr>
      <th>Concept</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><strong>Providers</strong></td>
      <td>In KSail <code>Providers</code> is an abstraction over the underlying provider in which the Kubernetes cluster is spun up.</td>
    </tr>
    <tr>
      <td><strong>Distributions</strong></td>
      <td>In KSail <code>Distributions</code> is an abstraction over the underlying Kubernetes distribution that is used to create the cluster.</td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNIs)</strong></td>
      <td>In KSail <code>CNIs</code> is an abstraction over the underlying Container Network Interface plugin that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Ingress Controllers</strong></td>
      <td>In KSail <code>Ingress Controllers</code> is an abstraction over the underlying Ingress Controller that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Waypoint Controllers</strong></td>
      <td>In KSail <code>Waypoint Controllers</code> is an abstraction over the underlying Waypoint Controller that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td>In KSail <code>Deployment Tools</code> is an abstraction over the underlying deployment tool that is used to deploy manifests to the cluster.</td>
    </tr>
    <tr>
      <td><strong>Secret Managers</strong></td>
      <td>In KSail <code>Secret Managers</code> is an abstraction over the underlying secret management tool that is used to manage secrets in Git.</td>
    </tr>
    <tr>
      <td><strong>Local Registry</strong></td>
      <td>In KSail <code>Local Registry</code> is the registry that is used to push and store OCI artifacts locally. It is used as a sync source for GitOps based <code>Deployment Tools</code>.</td>
    </tr>
    <tr>
      <td><strong>Mirror Registries</strong></td>
      <td>In KSail <code>Mirror Registries</code> is the registries used to proxy and cache images from upstream registries. It is used to ensure avoid pull rate limits.</td>
    </tr>
  </tbody>
</table>

## Providers

> [!NOTE]
> KSail is designed to be extensible and support multiple engines. However, at the moment, only the `Docker` engine is supported. O

`Providers` are the underlying providers in which the Kubernetes cluster is spun up. The engine is responsible for hosting and running the cluster.

### Docker

The `Docker` engine is a container runtime that is used to create and manage containers. It is the most widely used container runtime and is supported on all major operating systems.

Using `Docker` as the engine will create a Kubernetes cluster as container(s). This option will work with any container runtime that is compatible with Docker, such as `containerd` or `podman`.

## Distributions

### Native

The `Native` distribution is the default Kubernetes distribution provided by a specific `Provider`. In most cases this is a distribution with little to no modifications from the upstream Kubernetes release.

Below is the actual distribution used when using the `Native` distribution with the various engines.

- **Docker**: The `Native` distribution is `kind` because it has official support from a Kubernetes Special Interest Group (SIG).

### K3s

The `K3s` distribution is a lightweight Kubernetes distribution that is designed for resource-constrained environments. It's implementation depends on the `Provider` used.

- **Docker**: The `K3s` distribution is `k3d` which is a wrapper around `k3s` that runs in Docker containers.

## Container Network Interfaces (CNIs)

### None

> [!WARNING]
> This option is not supported yet.

### Default

The `Default` CNI is the default Container Network Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CNI plugin with limited features.

Because the `Native` distribution varies depending on the `Provider`, the CNI is determined by the `Provider` and `Distribution` you are using.

- **Docker + Native = Kind**: The `Default` CNI for `kind` is [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd)
- **Docker + K3s = K3d**: The `Default` CNI for `k3d` is [flannel](https://github.com/flannel-io/flannel)

### Cilium

Using the `Cilium` CNI will create a Kubernetes cluster with the Cilium CNI plugin installed.

## Ingress Controllers

### None

> [!WARNING]
> This option is not supported yet.

### Default

> [!WARNING]
> This option is not supported yet.

## Waypoint Controllers

### None

> [!WARNING]
> This option is not supported yet.

### Default

> [!WARNING]
> This option is not supported yet.

## Deployment Tools

### None

> [!WARNING]
> This option is not supported yet.

### Kubectl

> [!WARNING]
> This option is not supported yet.

### Flux

Using `Flux` as the deployment tool will create a Kubernetes cluster with `Flux` installed. By default it will use an `OCIRepository` source to sync the cluster with the local registry. It will also use a `FluxKustomization` to sync files referenced by the `k8s/kustomization.yaml` file.

### ArgoCD

> [!WARNING]
> This option is not supported yet.

## Secret Managers

### None

Using `None` as the secret manager will not create any encryption key nor bootstrap any secret management tool in the cluster.

### SOPS

Using `SOPS` as the secret manager will create an Age encryption key pair in the default `SOPS_AGE_KEY_FILE` location on the host machine. It will also create a secret in the cluster with the private key, to allow decryption of secrets before they are applied to the cluster.

## Local Registry

Using a `Local Registry` will create an official `registry:2` container in the cluster, that is configured to be accessible on `localhost`. This registry is used to push and store OCI artifacts for GitOps based `Deployment Tools`.

## Mirror Registries

Using `Mirror Registries` will create a `registry:2` container for each mirror registry that is configured. This registry is used to proxy and cache images from upstream registries, to avoid pull rate limits.
