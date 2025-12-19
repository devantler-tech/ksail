---
title: "cert-manager"
parent: Core Concepts
grand_parent: Overview
nav_order: 2
---

# cert-manager

[cert-manager](https://cert-manager.io/) is a Kubernetes controller for issuing and renewing TLS certificates.

KSail can optionally install cert-manager during cluster creation.

cert-manager is **disabled by default**.

## Enable during init

Enable cert-manager during `ksail cluster init` to persist the setting in `ksail.yaml`:

```bash
ksail cluster init --cert-manager Enabled
ksail cluster create
```

## Enable via configuration

Set the `spec.certManager` field in `ksail.yaml`:

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  certManager: Enabled
```

Then create the cluster:

```bash
ksail cluster create
```

## Enable via create flag

You can also enable cert-manager for a single `cluster create` invocation:

```bash
ksail cluster create --cert-manager Enabled
```

## Installation details

When enabled, KSail installs the Helm chart `jetstack/cert-manager` into the `cert-manager` namespace with `installCRDs: true`.

Verify it is running:

```bash
ksail workload get pods -n cert-manager
```
