---
title: Container Storage Interfaces (CSIs)
parent: Core Concepts
layout: default
nav_order: 3
---

# Container Storage Interfaces (CSIs)

## None

> [!WARNING]
> This option is not supported yet.

## Default

The `Default` CSI is the default Container Storage Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CSI plugin with limited features.

Below is a table of the default CSI plugins for each Kubernetes distribution supported by KSail:

| Distribution | CSI                                                                         |
| ------------ | --------------------------------------------------------------------------- |
| Kind         | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
| K3d          | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
