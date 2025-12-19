---
title: "Container Storage Interfaces (CSI)"
parent: Core Concepts
grand_parent: Overview
nav_order: 4
---

# Container Storage Interfaces (CSI)

Storage options determine how persistent volumes are provisioned for workloads. Configure CSI with `ksail cluster init --csi` or declaratively through `spec.csi` in `ksail.yaml`.

## Default

When you choose `Default`, KSail uses the distribution's built-in storage class. K3d includes [local-path-provisioner](https://github.com/rancher/local-path-provisioner) by default, while Kind does not have a default storage class. The `Default` option works well for simple development scenarios but offers limited features.

## LocalPathStorage

Explicitly installs [local-path-provisioner](https://github.com/rancher/local-path-provisioner) version v0.0.32. When configured:

- **On Kind clusters**: KSail installs the local-path-provisioner into the `local-path-storage` namespace and creates a `StorageClass` named `local-path` marked as the cluster default. This ensures PersistentVolumeClaims bind without additional configuration.

- **On K3d clusters**: KSail detects the built-in local-path-provisioner and performs no additional installation. K3d's included storage provisioner is already configured and ready to use.

This option is ideal when you want explicit control over storage provisioning in your local development environment.

## None

Choose `None` when you plan to supply your own storage controller. KSail skips CSI installation entirely, leaving PersistentVolumeClaims pending until your custom solution handles them.
