---
parent: Core Concepts
grand_parent: Overview
nav_order: 4
---

# Container Storage Interfaces (CSI)

Storage options determine how persistent volumes are provisioned for workloads. Configure CSI with `ksail cluster init --csi` or declaratively through `spec.csi` in `ksail.yaml`.

> **Note:** CSI configuration is defined in the spec but not yet fully implemented in cluster creation.

## Default

When you choose `Default`, KSail uses the distribution's built-in storage class. Both Kind and K3d use [local-path-provisioner](https://github.com/rancher/local-path-provisioner), which works well for development but offers limited features.

## LocalPathStorage

Explicitly installs local-path-provisioner. This option provides the same functionality as `Default` but makes the choice explicit.

## None

Choose `None` when you plan to supply your own storage controller. KSail skips CSI installation entirely, leaving PersistentVolumeClaims pending until your custom solution handles them.
