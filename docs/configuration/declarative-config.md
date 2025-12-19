---
title: Declarative Config
parent: Configuration
nav_order: 2
---

Every KSail project includes a `ksail.yaml` file that describes the desired cluster along with supporting distribution configs (`kind.yaml`, `k3d.yaml`). The CLI reads these files on every invocation and validates them before taking action.

## `ksail.yaml`

A minimal configuration looks like this:

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  distribution: Kind
  distributionConfig: kind.yaml
  sourceDirectory: k8s
  connection:
    kubeconfig: ~/.kube/config
    context: kind-local
  cni: Default
  metricsServer: Enabled
  certManager: Disabled
  localRegistry: Disabled
  gitOpsEngine: None
```

### Key fields inside `spec`

| Field                   | Type     | Allowed values                 | Purpose                                                                                                |
|-------------------------|----------|--------------------------------|--------------------------------------------------------------------------------------------------------|
| `distribution`          | enum     | `Kind`, `K3d`                  | Chooses the Kubernetes distribution.                                                                   |
| `distributionConfig`    | string   | File path                      | Points to the distribution-specific YAML (`kind.yaml` or `k3d.yaml`).                                  |
| `sourceDirectory`       | string   | Directory path                 | Location of the manifests for workload commands (default: `k8s`).                                     |
| `connection.kubeconfig` | string   | File path                      | Path to the kubeconfig used for cluster commands (default: `~/.kube/config`).                          |
| `connection.context`    | string   | kubeconfig context             | Context name for the cluster (e.g., `kind-<name>`).                                                   |
| `connection.timeout`    | duration | Go duration (e.g. `30s`, `5m`) | Optional timeout for cluster operations.                                                              |
| `cni`                   | enum     | `Default`, `Cilium`            | Container Network Interface to install (default: `Default`).                                          |
| `csi`                   | enum     | `Default`, `LocalPathStorage`  | Container Storage Interface (not yet implemented).                                                    |
| `metricsServer`         | enum     | `Enabled`, `Disabled`          | Install metrics-server (default: `Enabled`).                                                          |
| `certManager`           | enum     | `Enabled`, `Disabled`          | Install cert-manager (default: `Disabled`).                                                           |
| `localRegistry`         | enum     | `Enabled`, `Disabled`          | Provision a local OCI registry (default: `Disabled`).                                                 |
| `gitOpsEngine`          | enum     | `None`                         | GitOps engine (currently only `None` is supported; Flux/ArgoCD planned).                              |
| `options.*`             | object   | Provider-specific fields       | Advanced knobs for Kind, K3d, Flux, or Helm (currently placeholder).                                  |

> The CLI applies defaults for any field you omit. For example, if `cni` is not present, KSail uses `Default`, which uses the distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d).

### Updating the configuration safely

1. Edit `ksail.yaml` and commit the change.
2. Optionally override fields with environment variables like `KSAIL_SPEC_DISTRIBUTION=K3d` or flags like `ksail cluster create --metrics-server Disabled`.
3. Run `ksail cluster create` (or another command) to apply the configuration.

## Distribution configs

KSail stores distribution configuration alongside `ksail.yaml`:

- **`kind.yaml`** defines node layout, networking, and port mappings using the [Kind configuration format](https://kind.sigs.k8s.io/docs/user/configuration/). The default scaffold disables the built-in CNI so KSail can install your chosen provider.
- **`k3d.yaml`** follows the [K3d configuration format](https://k3d.io/stable/usage/configfile/). Edit this file to tweak load balancers, extra args, or node counts for K3s.

Use the `spec.distributionConfig` field in `ksail.yaml` to point to the desired file.

## Schema support and editor assistance

The KSail repository provides a JSON Schema for `ksail.yaml` validation and IntelliSense. Reference the schema directly from GitHub in your `ksail.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.dev/v1alpha1
kind: Cluster
...
```

This provides IDE validation and autocompletion without requiring a local copy of the schema. IDEs that support SchemaStore (including VS Code with the Red Hat YAML extension) will provide completions and validation automatically.
