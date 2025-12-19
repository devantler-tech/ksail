---
title: "Metrics Server"
parent: Core Concepts
grand_parent: Overview
nav_order: 9
---

# Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) aggregates CPU and memory usage across the cluster. Enable or disable it with `ksail cluster init --metrics-server` or by setting `spec.metricsServer` in `ksail.yaml`.

Metrics Server is **enabled by default**.

## Configuration

### During init

```bash
ksail cluster init --metrics-server Enabled  # default
ksail cluster init --metrics-server Disabled
```

### In ksail.yaml

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  metricsServer: Enabled  # or Disabled
```

### Override during create

```bash
ksail cluster create --metrics-server Disabled
```

## When to Use Metrics Server

Enable metrics server when:

- Testing Horizontal Pod Autoscaler (HPA)
- Using dashboard tools that display resource usage
- Working with alerts based on CPU/memory metrics

Disable it for:

- Minimal resource consumption during development
- Simple testing that doesn't require metrics
