# Declarative Config

Every KSail-Go project includes a `ksail.yaml` file that describes the desired cluster along with supporting distribution configs (`kind.yaml`, `k3d.yaml`). The CLI reads these files on every invocation, merges them with environment variables and flags, and validates the result before taking action.

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
  gitOpsEngine: None
```

### Key fields inside `spec`

| Field                   | Type     | Allowed values                 | Purpose                                                                                                |
|-------------------------|----------|--------------------------------|--------------------------------------------------------------------------------------------------------|
| `distribution`          | enum     | `Kind`, `K3d`                  | Chooses the container-based Kubernetes runtime.                                                        |
| `distributionConfig`    | string   | File path                      | Points to the distribution-specific YAML (`kind.yaml` or `k3d.yaml`).                                  |
| `sourceDirectory`       | string   | Directory path                 | Location of the GitOps manifests reconciled by Flux and the workload commands.                         |
| `connection.kubeconfig` | string   | File path                      | Path to the kubeconfig used for cluster lifecycle commands.                                            |
| `connection.context`    | string   | kubeconfig context             | Context name written by the scaffolder (for example `kind-<project>`).                                 |
| `connection.timeout`    | duration | Go duration (e.g. `30s`, `5m`) | Optional; apply when you want lifecycle commands to wait longer for operations.                        |
| `cni`                   | enum     | `Default`, `Cilium`, `Calico`  | Determines which CNI installer runs after the cluster provisions.                                      |
| `metricsServer`         | enum     | `Enabled`, `Disabled`          | Installs or removes metrics-server as part of post-provision steps.                                    |
| `gitOpsEngine`          | enum     | `None`                         | Reserved for future GitOps integrations.                                                               |
| `options.*`             | object   | Provider-specific fields       | Advanced knobs for Kind, K3d, Flux, or Helm. The scaffolder leaves them empty so you can opt-in later. |

> The CLI applies defaults for any field you omit. For example, if `cni` is not present, KSail-Go uses `Default`, which defers to the distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d).

### Updating the configuration safely

1. Edit `ksail.yaml` and commit the change so teammates pick up the new defaults.
2. Optionally override the same fields ad-hoc with environment variables such as `KSAIL_SPEC_DISTRIBUTION=K3d` or flags like `ksail cluster create --metrics-server Disabled`.
3. Run `ksail cluster create` (or another lifecycle command) to verify the new configuration.

## Distribution configs

KSail-Go stores the raw distribution files alongside `ksail.yaml`:

- **`kind.yaml`** defines node layout, networking, and port mappings using the upstream [Kind configuration format](https://kind.sigs.k8s.io/docs/user/configuration/). The default scaffold disables the built-in CNI so KSail-Go can install the provider you choose in `ksail.yaml`.
- **`k3d.yaml`** follows the [K3d `Simple` configuration format](https://k3d.io/stable/usage/configfile/). Edit this file when you want to tweak load balancers, extra args, or node counts for the lightweight K3s runtime.

Use the `spec.distributionConfig` field in `ksail.yaml` to point at whichever file you want KSail-Go to load. Teams often keep both files in version control and switch between them with a single flag or environment variable.

## Secrets and `.sops.yaml`

When you enable the SOPS secret manager during `ksail cluster init --secret-manager SOPS`, the scaffolder writes a `.sops.yaml` file and a `keys/` directory stub. The `ksail cipher` commands honour that configuration so you can encrypt/decrypt manifests without additional tooling. Update `.sops.yaml` whenever you rotate Age recipients or change the file selection rules.

## Schema support and editor assistance

The repository ships the JSON Schema at `schemas/ksail-config.schema.json`. Reference it from your YAML to receive IntelliSense:

```yaml
# yaml-language-server: $schema=../schemas/ksail-config.schema.json
apiVersion: ksail.dev/v1alpha1
kind: Cluster
...
```

Any IDE that understands SchemaStore (including VS Code with the Red Hat YAML extension) will pick up allowed values, enum completions, and validation hints automatically.
