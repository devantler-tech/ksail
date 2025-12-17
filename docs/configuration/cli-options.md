# CLI Options

The KSail-Go CLI exposes the same configuration surface everywhere: strongly typed flags that typically feed into `ksail.yaml`. Run `ksail <command> --help` to see the latest options, or use the quick references below when you need to remember which flag overrides which field.

## Quick reference

```bash
ksail --help                     # Top-level commands
ksail cluster init --help        # Project scaffolding flags
ksail cluster create --help      # Runtime overrides for an existing config
ksail cluster delete --help      # Clean-up options
ksail cluster connect -- --help  # Pass-through flags to k9s (note the `--`)
```

## Global flags

Some options affect CLI behavior rather than configuration.

| Flag       | Purpose                                                            |
| ---------- | ------------------------------------------------------------------ |
| `--timing` | Enable per-activity timing output for the current invocation only. |

When enabled, each successful activity prints a timing block immediately after the `✔` success line:

```text
✔ completion message
⏲ current: <duration>
  total:  <duration>
```

## Shared cluster flags

The cluster subcommands (`init`, `create`, `start`, `stop`, `delete`, `list`, `connect`) all bind to the same underlying configuration manager. Flags map directly to fields inside `ksail.yaml`, environment variables prefixed with `KSAIL_`, and sensible defaults.

| Flag                    | Short | Config key                   | Env variable                       | Default                   | Available on                                                                                                           |
| ----------------------- | ----- | ---------------------------- | ---------------------------------- | ------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `--distribution`        | `-d`  | `spec.distribution`          | `KSAIL_SPEC_DISTRIBUTION`          | `Kind`                    | `cluster init`, `cluster create`, `cluster start`, `cluster stop`, `cluster delete`, `cluster list`, `cluster connect` |
| `--distribution-config` | –     | `spec.distributionConfig`    | `KSAIL_SPEC_DISTRIBUTIONCONFIG`    | `kind.yaml`               | Same as above                                                                                                          |
| `--context`             | `-c`  | `spec.connection.context`    | `KSAIL_SPEC_CONNECTION_CONTEXT`    | Derived from distribution | Same as above                                                                                                          |
| `--kubeconfig`          | `-k`  | `spec.connection.kubeconfig` | `KSAIL_SPEC_CONNECTION_KUBECONFIG` | `~/.kube/config`          | Same as above                                                                                                          |
| `--source-directory`    | `-s`  | `spec.sourceDirectory`       | `KSAIL_SPEC_SOURCEDIRECTORY`       | `k8s`                     | `cluster init`                                                                                                         |
| `--cni`                 | –     | `spec.cni`                   | `KSAIL_SPEC_CNI`                   | `Default`                 | `cluster init`                                                                                                         |
| `--metrics-server`      | –     | `spec.metricsServer`         | `KSAIL_SPEC_METRICSSERVER`         | `Enabled`                 | `cluster init`, `cluster create`                                                                                       |
| `--gitops-engine`       | `-g`  | `spec.gitOpsEngine`          | `KSAIL_SPEC_GITOPSENGINE`          | `None`                    | `cluster init`, `cluster create`, `cluster start`, `cluster stop`, `cluster delete`, `cluster list`, `cluster connect` |

> **Environment variables:** Viper replaces dots (`.`) and hyphens (`-`) with underscores, so any field in `ksail.yaml` can be overridden with `KSAIL_<UPPERCASE_PATH>`. For example, `KSAIL_SPEC_CONNECTION_TIMEOUT=10m` sets the optional timeout even though there is no dedicated flag.

## Command reference

### `ksail cluster init`

Creates a new project in the current directory (or in `--output`). The command writes `ksail.yaml`, `kind.yaml`, and optional `k3d.yaml`, then seeds the Flux-ready `k8s/` tree.

- `--source-directory` controls where generated workloads live (`k8s` by default).
- `--cni` accepts `Default`, `Cilium`, or `Calico`. The value is stored in `ksail.yaml` for future `create` runs.
- `--metrics-server` toggles installing the Kubernetes metrics-server controller (`Enabled` / `Disabled`).
- `--gitops-engine` selects the GitOps engine (`None`, `Flux`, `ArgoCD`). When set to a GitOps engine, `cluster create` installs and configures the relevant controller.
- `--mirror-registry host=upstream` can be repeated to preconfigure local registry mirrors (for example `docker.io=https://registry-1.docker.io`).
- `--force` overwrites existing files, and `--output` chooses the target directory.

### `ksail cluster create`

Reads the committed configuration and provisions a cluster. Every flag listed in the shared table is available, plus:

- `--mirror-registry host=upstream` creates (or reuses) Docker registries before provisioning, then attaches them to the cluster network.
- `--metrics-server` lets you override the value stored in `ksail.yaml` when you need a one-off run.

The command automatically loads distribution configs (`kind.yaml` or `k3d.yaml`) and installs the requested CNI and metrics-server after the core cluster boots.

### `ksail cluster start` and `ksail cluster stop`

Resume or pause an existing cluster without rebuilding it. Both commands honour `--distribution`, `--distribution-config`, `--context`, and `--kubeconfig` so you can point at an alternate project directory or kubeconfig during operations.

### `ksail cluster delete`

Destroys the cluster defined in `ksail.yaml` and removes any mirror registries that were created. Use `--delete-volumes` when you want Docker volumes cleaned up as well.

### `ksail cluster list`

Shows the clusters currently managed by the selected distribution. Add `-a`/`--all` to query every supported distribution, even if it differs from the one in `ksail.yaml`.

### `ksail cluster info`

Proxies directly to `kubectl cluster-info`. Any arguments you pass are forwarded to `kubectl`, so standard flags such as `--context` or `--namespace` work as expected.

### `ksail cluster connect`

Launches [k9s](https://k9scli.io/) against the distribution and context defined in `ksail.yaml`. Add `--` before k9s flags to avoid Cobra parsing them (`ksail cluster connect -- --namespace flux-system`).

## Workload and cipher commands

Other command groups (`ksail workload`, `ksail cipher`, and the generators under `ksail workload gen`) inherit
Kubernetes-native semantics. They forward all flags to the underlying tooling (`kubectl`, Helm, or SOPS), so rely on
the upstream help output for exhaustive flag listings. The configuration rules above still apply: any cluster-related
override you set for lifecycle commands carries across to workload reconciliation and secret management.
