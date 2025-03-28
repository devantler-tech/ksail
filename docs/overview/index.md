---
title: Overview
layout: default
nav_order: 1
---

# Overview

KSail is an SDK for Kubernetes allowing you to easily create, manage, and dismantle Kubernetes clusters in various providers (currently Docker). It is built on top of popular Kubernetes tools with the goal of improving the developer experience (DX) when working with Kubernetes.

![KSail Architecture](../images/architecture.drawio.png)

## Key Features

- **Provision and Manage Clusters:**  Easily create, manage, and dismantle Kubernetes clusters across supported providers.
- **Customizable Cluster Components:** Configure essential components like CNI, Ingress, Waypoint, and other add-ons to suit your needs.
- **Deployment of manifests:** Deploy manifests to clusters seamlessly using popular deployment tools.
- **Debugging and Troubleshooting:** Debug and troubleshoot clusters with built-in tools and commands for quick issue resolution.
- **Secure Secret Management:** Manage secrets securely in Git.
- **Mirror Registry Management:** Set up and manage mirror registries to optimize image pulling and reduce external dependencies.
- **Cluster Validation:** Validate cluster configurations and manifests.
- **Extensible Architecture:** Extend KSail sub-projects with provisioners for custom providers, distributions, and components.
