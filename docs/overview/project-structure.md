---
title: Project Structure
parent: Overview
nav_order: 1
---

# Project Structure

When you create a new project with `ksail init`, it will generate a set of files and directories to get you started. The generated project structure depends on how you configure the project via the declarative config and the CLI options.

Below is a typical project structure for a KSail project:

```shell
├── ksail.yaml # KSail configuration file
├── <distribution>.yaml # Distribution configuration file
└── k8s # Kubernetes manifests
    └── kustomization.yaml # Kustomize index file
```

If you choose to enable the Secret Manager, the project will also include a `.sops.yaml` file that configures the SOPS secret management tool to be able to encrypt and decrypt secrets.

## Kustomize-based

KSail generates projects that follow a [Kustomize](https://kubernetes-sigs.github.io/kustomize/)-based structure. Kustomize is a tool designed to simplify and manage Kubernetes YAML configurations. At the core of every KSail project is the `k8s/kustomization.yaml` file, which acts as the main index for the project. This file defines the resources and configurations that will be applied to your Kubernetes cluster.

Using Kustomize, you can organize your project into reusable "bases" and apply "patches" to customize configurations for different environments or clusters. For example, you might have a base configuration for a service and then apply patches to adjust settings for development, staging, or production clusters. By referencing different `kustomization.yaml` files, you can easily switch between configurations, ensuring flexibility and consistency across multiple clusters.

You can set the index `kustomization.yaml` file with the `--kustomization-path` option or by setting the `spec.project.kustomizationPath` field in the KSail configuration file.
