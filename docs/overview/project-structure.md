---
title: Project Structure
parent: Overview
layout: default
nav_order: 2
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
