# Cert-Manager

[cert-manager](https://cert-manager.io/) is the de-facto Kubernetes controller for issuing and renewing TLS certificates.

KSail-Go can optionally install cert-manager during `ksail cluster create`.

Cert-manager is **disabled by default**.

## Recommended: enable during init

Most workflows should enable cert-manager during `ksail cluster init`, so the setting is persisted into `ksail.yaml` and applies to subsequent `ksail cluster create` runs.

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

## Enable via create flag (one-off override)

You can also enable cert-manager for a single `cluster create` invocation:

```bash
ksail cluster create --cert-manager Enabled
```

## Installation details

When enabled, KSail-Go installs the upstream Helm chart `jetstack/cert-manager` into the `cert-manager` namespace and sets `installCRDs: true`.

Verify it is running:

```bash
ksail workload get pods -n cert-manager
```
