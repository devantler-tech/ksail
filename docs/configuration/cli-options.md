---
parent: Configuration
nav_order: 1
---

# CLI Options

KSail CLI provides strongly typed flags that map to `ksail.yaml` configuration. Run `ksail <command> --help` to see the latest options, or use the quick references below.

## Quick reference

```bash
ksail --help                     # Top-level commands
ksail cluster init --help        # Project scaffolding flags
ksail cluster create --help      # Cluster creation options
ksail cluster delete --help      # Clean-up options
ksail cluster connect --help     # k9s connection options
```

## Global flags

Some options affect CLI behavior rather than configuration.

| Flag       | Purpose                                                            |
|------------|--------------------------------------------------------------------|
| `--timing` | Enable per-activity timing output for the current invocation only. |

When enabled, each successful activity prints a timing block immediately after the `✔` success line:

```text
✔ completion message
⏲ current: <duration>
  total:  <duration>
```

## Shared cluster flags

The cluster subcommands bind to the same configuration. Flags map to fields in `ksail.yaml` and environment variables prefixed with `KSAIL_`.

| Flag                    | Short | Config key                   | Default                   | Available on                         |
|-------------------------|-------|------------------------------|---------------------------|--------------------------------------|
| `--distribution`        | `-d`  | `spec.distribution`          | `Kind`                    | `cluster init`                       |
| `--distribution-config` | –     | `spec.distributionConfig`    | `kind.yaml`               | `cluster init`                       |
| `--context`             | `-c`  | `spec.connection.context`    | Derived from distribution | `cluster init`                       |
| `--kubeconfig`          | `-k`  | `spec.connection.kubeconfig` | `~/.kube/config`          | `cluster init`                       |
| `--source-directory`    | `-s`  | `spec.sourceDirectory`       | `k8s`                     | `cluster init`                       |
| `--cni`                 | –     | `spec.cni`                   | `Default`                 | `cluster init`                       |
| `--csi`                 | –     | `spec.csi`                   | `Default`                 | `cluster init` (not yet implemented) |
| `--metrics-server`      | –     | `spec.metricsServer`         | `Enabled`                 | `cluster init`, `cluster create`     |
| `--cert-manager`        | –     | `spec.certManager`           | `Disabled`                | `cluster init`, `cluster create`     |
| `--local-registry`      | –     | `spec.localRegistry`         | `Disabled`                | `cluster init`                       |
| `--local-registry-port` | –     | (port configuration)         | `5111`                    | `cluster init`                       |
| `--gitops-engine`       | `-g`  | `spec.gitOpsEngine`          | `None`                    | `cluster init`                       |
| `--flux-interval`       | –     | (Flux reconciliation)        | `1m0s`                    | `cluster init`                       |
| `--mirror-registry`     | –     | (multiple allowed)           | None                      | `cluster init`                       |

> **Note:** Environment variables follow the pattern `KSAIL_SPEC_<FIELD>` where field names are uppercase with underscores.

## Command reference

### `ksail cluster init`

Creates a new project in the current directory (or in `--output`). The command writes `ksail.yaml`, `kind.yaml` or `k3d.yaml`, and the `k8s/` tree for workloads.

- `--distribution` chooses between `Kind` and `K3d`
- `--source-directory` controls where workloads live (`k8s` by default)
- `--cni` accepts `Default`, `Cilium`, or `None`
- `--metrics-server` toggles metrics-server installation (`Enabled` / `Disabled`)
- `--cert-manager` toggles cert-manager installation (`Enabled` / `Disabled`)
- `--local-registry` provisions a local OCI registry (`Enabled` / `Disabled`)
- `--local-registry-port` sets the host port for the local registry (default `5111`)
- `--gitops-engine` currently only supports `None` (Flux and ArgoCD planned)
- `--flux-interval` sets Flux reconciliation interval (e.g., `1m`, `30s`)
- `--mirror-registry host=upstream` can be repeated to configure registry mirrors
- `--force` overwrites existing files
- `--output` chooses the target directory

### `ksail cluster create`

Reads `ksail.yaml` and provisions a cluster. The command loads the distribution config and installs the CNI and metrics-server after the core cluster boots.

- `--metrics-server` overrides the value in `ksail.yaml`
- `--cert-manager` overrides the value in `ksail.yaml`

### `ksail cluster start` and `ksail cluster stop`

Resume or pause an existing cluster. Both commands use the configuration from `ksail.yaml`.

### `ksail cluster delete`

Destroys the cluster defined in `ksail.yaml` and cleans up resources. Use `--delete-volumes` to remove Docker volumes as well.

### `ksail cluster list`

Shows clusters managed by the current distribution. Add `-a`/`--all` to query all supported distributions.

### `ksail cluster info`

Displays cluster information using `kubectl cluster-info`. Arguments are forwarded to `kubectl`.

### `ksail cluster connect`

Launches [k9s](https://k9scli.io/) against the cluster defined in `ksail.yaml`.

## Workload and cipher commands

The `ksail workload` commands wrap `kubectl` and Helm, forwarding flags to the underlying tools. Use `ksail workload <command> --help` for command-specific options.

The `ksail cipher` commands provide SOPS integration for encrypting and decrypting files. See `ksail cipher --help` for available operations.
