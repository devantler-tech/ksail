---
title: Deployment Tools
parent: Core Concepts
nav_order: 6
---

# Deployment Tools

`Deployment Tools` refer to the tools that are used to deploy manifests to the cluster. This can be a GitOps based deployment tool, or an apply based deployment tool. The `Deployment Tool` is responsible for managing the deployment of manifests to the cluster and synchronizing the cluster state with the desired state defined in the manifests.

## Kubectl

Using [Kubectl](https://kubernetes.io/docs/reference/kubectl/overview/) as the deployment tool enables applying kustomizations and waiting for their completion. It utilizes `kubectl apply -k` with `--prune` and the alpha feature `--applyset` to manage deployments via a central `kustomization.yaml` file, allowing you to add or remove resources by updating the file. Additionally, `kubectl rollout status -k` provides deployment status updates.

Kubectl is the default deployment tool for ksail, because it is built on official Kubernetes tooling, developed and maintained by the Kubernetes project and community. This makes it a reliable and straightforward choice, especially for small projects or testing new features. However, it does not provide GitOps-based deployment capabilities.

## Flux

Using [Flux](https://fluxcd.io/) as the deployment tool will create a Kubernetes cluster with `Flux` installed. By default it will use an `OCIRepository` source to sync the cluster with the local registry. It will also use a `FluxKustomization` to sync files referenced by the `k8s/kustomization.yaml` file.

## ArgoCD

> [!WARNING]
> This option is not supported yet.

Using [ArgoCD](https://argoproj.github.io/argo-cd/) as the deployment tool will create a Kubernetes cluster with `ArgoCD` installed. It provides a declarative GitOps approach to continuous delivery, allowing you to manage your Kubernetes resources by synchronizing them with external state sources.
