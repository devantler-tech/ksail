---
title: Overview
layout: default
nav_order: 1
---

# Overview

> [!NOTE]
> Currently, KSail supports `Docker` as a provider for local development. Support for other providers is planned for the future.

KSail is an SDK for Kubernetes allowing you to easily create, manage, and dismantle Kubernetes clusters in various providers. It is built on top of popular Kubernetes tools with the goal of improving the developer experience (DX) when working with Kubernetes.

![KSail Architecture](../images/architecture.drawio.png)

## Key Features

- **Initialize projects:** Initialize new kubernetes projects with a single command.
- **Provision and Manage Clusters:**  Easily create, manage, and dismantle Kubernetes clusters across supported providers.
- **Customizable Cluster Components:** Configure essential components like CNI, Ingress, Gateway, and other add-ons to suit your needs.
- **Deployment of manifests:** Deploy manifests to clusters seamlessly using popular deployment tools.
- **Debugging and Troubleshooting:** Debug and troubleshoot clusters with built-in tools and commands for quick issue resolution.
- **Generate manifests:** Generate Kubernetes manifests for various components and configurations.
- **Secure Secret Management:** Manage secrets securely in Git.
- **Mirror Registry Management:** Set up and manage mirror registries to optimize image pulling and reduce external dependencies.
- **Cluster Validation:** Validate cluster configurations and manifests.
- **Extensible Architecture:** Extend KSail sub-projects with provisioners for custom providers, distributions, and components.
