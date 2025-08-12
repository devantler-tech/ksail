---
title: Container Storage Interfaces (CSIs)
parent: Core Concepts
nav_order: 3
---

# Container Storage Interfaces (CSIs)

## Default

The `Default` CSI is the default Container Storage Interface plugin that is bundled with the Kubernetes distribution you are using. It is often a basic CSI plugin with limited features.

Below is a table of the default CSI plugins for each Kubernetes distribution supported by KSail:

| Distribution | CSI                                                                         |
| ------------ | --------------------------------------------------------------------------- |
| Kind         | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |
| K3d          | [local-path-provisioner](https://github.com/rancher/local-path-provisioner) |

## Local Path Provisioner

The [`local-path-provisioner`](https://github.com/rancher/local-path-provisioner) is a simple and lightweight CSI plugin that allows you to use local storage on your Kubernetes nodes. It creates a local path on each node and uses it as a persistent volume for your applications. This is useful for development and testing purposes, but not recommended for production environments.

## None

The `None` CSI option means that no Container Storage Interface plugin will be installed in your Kubernetes cluster. This is useful if you want to manage storage manually or use a different storage solution that does not require a CSI plugin.
