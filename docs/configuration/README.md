---
title: Configuration
nav_order: 3
has_children: true
---

KSail keeps cluster configuration reproducible through well-defined configuration files. Run `ksail cluster init` once, commit the generated files, and the team can rely on consistent defaults.

## Configuration surfaces

- [CLI options](cli-options.md) – Override configuration at runtime with flags for one-off changes
- [Declarative config](declarative-config.md) – Define settings in `ksail.yaml`, `kind.yaml`, and `k3d.yaml` for persistent configuration
- Workloads – The generated `k8s/` directory contains Kustomize-based manifests; see [project structure](../overview/project-structure.md)

## Precedence and loading

Configuration is resolved in the following order (highest precedence first):

1. CLI flags supplied on the command
2. Environment variables prefixed with `KSAIL_` (e.g., `KSAIL_SPEC_DISTRIBUTION=K3d`)
3. The nearest `ksail.yaml` in the current or parent directories
4. Built-in defaults

## Common defaults

| Setting                      | Default               | Notes                                                                |
| ---------------------------- | --------------------- | -------------------------------------------------------------------- |
| `spec.distribution`          | `Kind`                | Switch to `K3d` for the K3s-based runtime.                          |
| `spec.distributionConfig`    | `kind.yaml`           | Points to the distribution configuration file.                       |
| `spec.sourceDirectory`       | `k8s`                 | Directory containing workload manifests.                             |
| `spec.connection.kubeconfig` | `~/.kube/config`      | Path to kubeconfig file.                                             |
| `spec.connection.context`    | Derived from cluster  | Kind: `kind-<name>`, K3d: `k3d-<name>`.                              |
| `spec.cni`                   | `Default`             | Choose `Cilium` or `None` for different networking.                  |
| `spec.metricsServer`         | `Enabled`             | Set to `Disabled` to skip metrics-server installation.               |
| `spec.certManager`           | `Disabled`            | Set to `Enabled` to install cert-manager.                            |
| `spec.localRegistry`         | `Disabled`            | Set to `Enabled` to provision a local OCI registry.                  |
| `spec.gitOpsEngine`          | `None`                | Currently only `None` supported; Flux/ArgoCD planned.                |

## When to edit what

- **Use CLI flags** for temporary overrides (e.g., `ksail cluster create --metrics-server Disabled`)
- **Edit `ksail.yaml`** for project-wide defaults that all commands will use
- **Edit `kind.yaml` or `k3d.yaml`** for distribution-specific settings like node counts or ports

These configuration layers allow you to balance version-controlled defaults with flexible runtime overrides.
