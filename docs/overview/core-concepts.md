---
title: Core Concepts
parent: Overview
layout: default
nav_order: 0
---

# Core Concepts

Even though KSail is designed to provide a simple and easy to use interface, it is important to understand the core concepts of KSail, Kubernetes and the related cloud-native tooling. This document will take you through the core concepts of KSail, as well as the underlying technologies and tooling that KSail is built on top of.

It is recommended that you familiarize yourself with the following topics if you really want to understand how KSail works and how to use it effectively.

- [Docker](https://docs.docker.com/)
- [Kubernetes](https://kubernetes.io/docs/home/)
- [Kustomize](https://kubernetes-sigs.github.io/kustomize/)
- [GitOps](https://www.gitops.tech/)
- [Cloud Native](https://www.cncf.io/)

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
      <td>In KSail <code>Providers</code> is an abstraction over local, on-prem or cloud providers in which a Kubernetes cluster can be spun up.</td>
    </tr>
    <tr>
      <td><strong>Distributions</strong></td>
      <td>In KSail <code>Distributions</code> is an abstraction over the underlying Kubernetes distribution that is used to create the cluster.</td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNIs)</strong></td>
      <td><code>CNIs</code> refers to Container Network Interface plugins that facilitate networking for containers in a Kubernetes cluster.</td>
    </tr>
    <tr>
      <td><strong>Ingress Controllers</strong></td>
      <td><code>Ingress Controllers</code> refers to the controllers that manage ingress resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster.</td>
    </tr>
    <tr>
      <td><strong>Gateway Controllers</strong></td>
      <td><code>Gateway Controllers</code> refers to the controllers that manage gateway resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster.</td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td>In KSail <code>Deployment Tools</code> is an abstraction over the underlying deployment tool that is used to deploy manifests to the cluster.</td>
    </tr>
    <tr>
      <td><strong>Secret Manager</strong></td>
      <td>In KSail <code>Secret Manager</code> is SOPS. It is used to work with secrets in the project, and to help keep sensitive values encrypted in Git.</td>
    </tr>
    <tr>
      <td><strong>Local Registry</strong></td>
      <td>In KSail <code>Local Registry</code> is the registry that is used to push and store OCI and Docker images. It is used to store images locally for GitOps based deployment tools, and manually uploaded images.</td>
    </tr>
    <tr>
      <td><strong>Mirror Registries</strong></td>
      <td><code>Mirror Registries</code> refers to registries that are used to proxy and cache images from upstream registries. This is used to avoid pull rate limits and to speed up image pulls.</td>
    </tr>
  </tbody>
</table>

## Providers

> [!NOTE]
> KSail is designed to be extensible and support multiple providers. However, at the moment, only the `Docker` provider is supported.

`Providers` refer to the underlying infrastructure that is used to create the Kubernetes cluster. This can be a local provider, on-premises provider or a cloud provider. The `Provider` is responsible for creating and managing the underlying infrastructure that is used to run the Kubernetes cluster.

### [Docker](https://www.docker.com/)

The `Docker` provider is a container runtime that is used to create and manage containers. It is the most widely used container runtime and it is supported on all major operating systems.

Using `Docker` as a provider will create a Kubernetes cluster as container(s).

## Distributions

`Distributions` refer to the underlying Kubernetes distribution that is used to create the cluster. This can be the providers native distribution, or some other distribution that is compatible with the provider. The `Distribution` is responsible for providing the Kubernetes API and the underlying components that are used to run the cluster.

### Native

The `Native` distribution is the default Kubernetes distribution provided by a specific `Provider`. In most cases this is a distribution that is optimized for the provider and is designed to work seamlessly with the provider's infrastructure.

Below is the actual distribution used when using the `Native` distribution with the various engines.

- **Docker**: The `Native` distribution is [`kind`](https://kind.sigs.k8s.io/). This is a wrapper around the official Kubernetes distribution that is designed to run in Docker containers. This was decided as the native distribution because it has official support from a [Kubernetes Special Interest Group (SIG)](https://github.com/kubernetes/community/blob/master/sig-list.md).

### [K3s](https://k3s.io/)

The `K3s` distribution is a lightweight Kubernetes distribution that is designed for resource-constrained environments. Its implementation depends on the `Provider` used.

- **Docker**: The `K3s` distribution is `k3d`. This is a wrapper around `k3s` that runs in Docker containers.

## Container Network Interfaces (CNIs)

### None

> [!WARNING]
> This option is not supported yet.

### Default

The `Default` CNI is the default Container Network Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CNI plugin with limited features.

Because the `Native` distribution varies depending on the `Provider`, the CNI is determined by the `Provider` and `Distribution` you are using.

- **Docker + Native = Kind**: The `Default` CNI for `kind` is [kindnetd](https://github.com/kubernetes-sigs/kind/tree/main/images/kindnetd)
- **Docker + K3s = K3d**: The `Default` CNI for `k3d` is [flannel](https://github.com/flannel-io/flannel)

### [Cilium](https://cilium.io/)

Using the `Cilium` CNI will create a Kubernetes cluster with the Cilium CNI plugin installed. It works with all combinations of `Provider` and `Distribution` that are supported by KSail.

## Ingress Controllers

`Ingress Controllers` refer to the controllers that manage ingress resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster. The `Ingress Controller` is responsible for managing the ingress resources and providing a way to route external traffic to the appropriate services.

### None

> [!WARNING]
> This option is not supported yet.

### Default

> [!WARNING]
> This option is not supported yet.

## Gateway Controllers

> [!NOTE]
> The [Gateway API](https://gateway-api.sigs.k8s.io) is a fairly new API that is designed to supercede the Ingress API. It solves some of the limitations of the Ingress API, but it is not yet widely adopted, and may have limited support in the implementation you are using.

`Gateway Controllers` refer to the controllers that manage gateway resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster. The `Gateway Controller` is responsible for managing the gateway resources and providing a way to route external traffic to the appropriate services.

### None

> [!WARNING]
> This option is not supported yet.

### Default

> [!WARNING]
> This option is not supported yet.

## Deployment Tools

`Deployment Tools` refer to the tools that are used to deploy manifests to the cluster. This can be a GitOps based deployment tool, or an apply based deployment tool. The `Deployment Tool` is responsible for managing the deployment of manifests to the cluster and synchronizing the cluster state with the desired state defined in the manifests.

### [Kubectl](https://kubernetes.io/docs/reference/kubectl/overview/)

> [!WARNING]
> This option is not supported yet.

### [Flux](https://fluxcd.io/)

Using `Flux` as the deployment tool will create a Kubernetes cluster with `Flux` installed. By default it will use an `OCIRepository` source to sync the cluster with the local registry. It will also use a `FluxKustomization` to sync files referenced by the `k8s/kustomization.yaml` file.

### [ArgoCD](https://argo-cd.readthedocs.io/en/stable/)

> [!WARNING]
> This option is not supported yet.

## Secret Manager

> [!TIP]
> The `Secret Manager` is disabled by default, as it is considered an advanced feature. If you want to use it, you should enable it when initializing the project. This ensures that a new encryption key is created on your system, and that the secret manager is correctly configured with your chosen distribution and deployment tool. If you do not enable it, you will have to manually configure the secret manager later.

KSail uses [`SOPS`](https://getsops.io) as the secret manager. This is a tool that is used to encrypt and decrypt secrets in a way that is compatible with GitOps based deployment tools. It is designed to work with Git and provides a way to keep sensitive values encrypted in Git.

KSail ensures that a private key is securely stored in the cluster, allowing for seamless decryption of secrets when they are applied to the cluster.

> [!NOTE]
> `SOPS` supports both `PGP` and `Age` key pairs, but for now, KSail only supports `Age` key pairs. This is because `Age` is a newer and simpler encryption format that is designed to be easy to use and understand. It is also more secure than `PGP` because it solely uses modern cryptography and does not rely on any legacy algorithms or protocols.

## Local Registry

> [!WARNING]
> Using remote registries as a local registry is not supported yet. This means that remote registries cannot be used in place of a local registry for pushing and storing images.
>
> Support for unauthenticated access to upstream registries is also unsupported. This means that you cannot setup authentication in front of the local registry.
>
> These are limitations of the current implementation and will be fixed in the future.

`Local Registry` refers to the registry that is used to push and store OCI and Docker images. The primary use case for the `Local Registry` is to store OCI artifacts with manifests for GitOps based deployment tools, but it also allows you to push and store local images if you want to test out custom Docker images in Kubernetes, which are not available in upstream registries.

Using a `Local Registry` will create an official `registry:2` container in a specified provider. The registry is configured to be accessible on `localhost`.

## Mirror Registries

> [!WARNING]
> Remote `Mirror Registries` are not supported yet. This means that remote registries cannot be used as mirrors for upstream registries.
>
> Support for unauthenticated access to upstream registries is also unsupported. This means that you cannot setup authentication in front of the mirror registry, or to authenticate from the mirror registry to the upstream registry.
>
> Lastly, mirror registries do not support secure connections to upstream registries with TLS.
>
> These are limitations of the current implementation and will be fixed in the future.

`Mirror Registries` refer to registries that are used to proxy and cache images from upstream registries. This is used to avoid pull rate limits and to speed up image pulls.

Using `Mirror Registries` will create a `registry:2` container for each mirror registry that is configured. You can configure as many mirror registries as you need.
