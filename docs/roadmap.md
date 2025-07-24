---
title: Roadmap
nav_order: 5
---

# Roadmap

The roadmap for KSail details the planned features and improvements for the project. The roadmap is not a exhaustive list of all the changes that will be made to KSail, but rather a high-level overview of the more prominent features and improvements that are planned for the project.

## 2024

- [x] Large refactor to extract all generic functionality into separate libraries. For example the template engine, the different APIs for embedded binaries, and implementations for e.g. provisioning and managing clusters.
- [x] KSail YAML Config to enable setting defaults for KSail that are overwritable by CLI flags. This will enable simplifying the CLI commands, such that users do not have to specify the same flags over and over again.
- [x] Bug fixes and small convenience features. For example there is a few issues around the hardcoded port range used for the OCI registry and pull-through caches, as well as some issues with.
- [x] KSail Gen to generate Kubernetes manifests, config files, and more. This will enable users to generate the files they need for their projects, without having to write them from scratch. This will be an iterative process, where initially only a few resources will be supported, and more will be added over time.
- [x] Kind support to enable users to create and manage GitOps-enabled Kubernetes clusters provisioned with Kind. This is important for users that prefer to use Kind over K3d.

## 2025

- [x] The `ksail sops` command to manage secrets in GitOps-enabled Kubernetes clusters. This will enable users to manage secrets in their clusters in a secure way, without having to worry about secrets being exposed in their Git repositories.
- [x] Mirror registry support for kind.
- [x] Cilium support to enable users to create clusters with Cilium as the default CNI.
- [x] Traefik.me support to enable users to make services accessible through ingresses without having to set up a DNS server locally.
- [x] Podman support to enable users to create and manage GitOps-enabled Kubernetes clusters provisioned with Podman. This is important for users that prefer to use Podman over Docker.
- [ ] The `ksail diff` command to show the the differences between the current state of the cluster and the desired state before applying the changes. This can be useful to see all the hidden changes that are applied by e.g. HelmRelease resources.
- [ ] Talos in Docker support to enable users to create and manage GitOps-enabled Kubernetes clusters provisioned with Talos Linux in Docker. This is important for users that prefer to use Talos over K3d.
- [ ] ArgoCD support to enable users to manage their clusters through ArgoCDs GitOps engine. This is important to support a wide range of use cases.

## Features I'm considering

- [ ] Azure support to enable users to create and manage Kubernetes clusters on Azure. This would be useful for large organizations, and clusters that cannot be developed on locally due to resource constraints.
- [ ] GCP support to enable users to create and manage Kubernetes clusters on GCP. This would be useful for large organizations, and clusters that cannot be developed on locally due to resource constraints.
- [ ] AWS support to enable users to create and manage Kubernetes clusters on AWS. This would be useful for large organizations, and clusters that cannot be developed on locally due to resource constraints.
- [ ] Omni support to enable users to create and manage Kubernetes clusters on Omni. This would be useful for large organizations, and clusters that cannot be developed on locally due to resource constraints.
- [ ] Cluster API support to enable users to create and manage GitOps-enabled Kubernetes clusters provisioned with Cluster API. This is important for users that operate large Kubernetes clusters.
- [ ] VCluster support so users can create and manage KSail clusters in a virtual cluster environment on existing Kubernetes clusters. This would be useful for scaling KSail clusters horizontally, as KSail clusters are currently limited to the resources available on the host machine, or the resources allocated to the Docker daemon. Furthermore this would make sure that KSail in CI scales well, whereas today it quickly reaches the limits of for example GitHub's hosted runners.
