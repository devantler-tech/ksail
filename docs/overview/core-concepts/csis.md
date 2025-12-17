# Container Storage Interfaces (CSI)

Storage options determine how persistent volumes are provisioned for workloads. Configure CSI during initialization with `ksail cluster init --csi` or declaratively through `spec.cluster.csi`.

## Default

When you choose `Default`, KSail-Go keeps the distribution's builtin storage class. Today both Kind and K3d rely on [local-path-provisioner](https://github.com/rancher/local-path-provisioner), which works well for day-to-day development but offers limited features.

> **Distribution defaults:** Both Kind and K3d provision local-path-provisioner when `Default` is selected.

## Local Path Provisioner

Selecting `LocalPathProvisioner` installs the same controller explicitly. This is helpful when you opt into the `None` distribution default elsewhere but still want quick local storage. Expect host-path-backed volumes, so avoid running stateful workloads that require high availability.

## None

Choose `None` when you plan to supply your own storage controller or connect to external persistent volumes. KSail-Go skips CSI installation entirely, leaving all PersistentVolumeClaims pending until your custom solution handles them.
