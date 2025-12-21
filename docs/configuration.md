---
title: "Configuration"
nav_order: 3
---

# Configuration

KSail uses declarative YAML files and CLI overrides for reproducible cluster configuration. Run `ksail cluster init` to generate files that can be committed and shared.

## Configuration Precedence

Configuration is resolved (highest to lowest):

1. CLI flags (e.g., `--metrics-server Disabled`)
2. Environment variables with `KSAIL_` prefix (e.g., `KSAIL_SPEC_DISTRIBUTION=K3d`)
3. Nearest `ksail.yaml` in current or parent directories
4. Built-in defaults

## Declarative Configuration

Each KSail project includes a `ksail.yaml` describing the desired cluster.

### Example ksail.yaml

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  editor: code --wait
  cluster:
    distribution: Kind
    distributionConfig: kind.yaml
    connection:
      kubeconfig: ~/.kube/config
      context: kind-local
    cni: Default
    metricsServer: Enabled
    certManager: Disabled
    localRegistry: Disabled
    gitOpsEngine: None
  workload:
    sourceDirectory: k8s
    validateOnPush: false
```

### Configuration Fields

| Field                           | Type     | Default              | Values                         | Description                                                        |
|---------------------------------|----------|----------------------|--------------------------------|--------------------------------------------------------------------|
| `editor`                        | string   | –                    | Command with args              | Editor for interactive workflows (e.g. `code --wait`, `vim`).      |
| `cluster.distribution`          | enum     | `Kind`               | `Kind`, `K3d`                  | Kubernetes distribution to use.                                    |
| `cluster.distributionConfig`    | string   | `kind.yaml`          | File path                      | Path to distribution-specific YAML (`kind.yaml` or `k3d.yaml`).    |
| `cluster.connection.kubeconfig` | string   | `~/.kube/config`     | File path                      | Path to kubeconfig file.                                           |
| `cluster.connection.context`    | string   | Derived from cluster | kubeconfig context             | Context name (Kind: `kind-<name>`, K3d: `k3d-<name>`).             |
| `cluster.connection.timeout`    | duration | –                    | Go duration (e.g. `30s`, `5m`) | Optional timeout for cluster operations.                           |
| `cluster.cni`                   | enum     | `Default`            | `Default`, `Cilium`, `None`    | Container Network Interface to install.                            |
| `cluster.csi`                   | enum     | `Default`            | `Default`, `LocalPathStorage`  | Container Storage Interface (not yet implemented).                 |
| `cluster.metricsServer`         | enum     | `Enabled`            | `Enabled`, `Disabled`          | Install metrics-server for resource metrics.                       |
| `cluster.certManager`           | enum     | `Disabled`           | `Enabled`, `Disabled`          | Install cert-manager for TLS certificates.                         |
| `cluster.localRegistry`         | enum     | `Disabled`           | `Enabled`, `Disabled`          | Provision local OCI registry.                                      |
| `cluster.gitOpsEngine`          | enum     | `None`               | `None`, `Flux`, `ArgoCD`       | GitOps engine to install.                                          |
| `workload.sourceDirectory`      | string   | `k8s`                | Directory path                 | Location of workload manifests.                                    |
| `workload.validateOnPush`       | boolean  | `false`              | `true`, `false`                | Automatically validate manifests before pushing to local registry. |

> Omitted fields use defaults (e.g., `cluster.cni` defaults to `Default`).

### Distribution Configs

Distribution configuration sits alongside `ksail.yaml`:

- **`kind.yaml`** – [Kind configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
- **`k3d.yaml`** – [K3d configuration](https://k3d.io/stable/usage/configfile/)

Reference via `spec.cluster.distributionConfig`.

### Schema Support

The KSail repository provides a JSON Schema for validation and IntelliSense. Reference it in your `ksail.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.dev/v1alpha1
kind: Cluster
...
```

IDEs with YAML language support (like VS Code with the Red Hat YAML extension) will provide completions and validation automatically.

## CLI Options

All configuration fields can be overridden via CLI flags. Run `ksail <command> --help` to see the latest options.

### Quick Reference

```bash
ksail --help                     # Top-level commands
ksail cluster init --help        # Project scaffolding flags
ksail cluster create --help      # Cluster creation options
ksail cluster delete --help      # Clean-up options
ksail workload validate --help   # Manifest validation options
```

### Global Flags

| Flag       | Purpose                                              |
|------------|------------------------------------------------------|
| `--timing` | Enable timing output for the current invocation only |

### Cluster Flags

All cluster commands support these flags (availability varies by command):

| Flag                    | Short | Config Field                         | Default          | Commands                     |
|-------------------------|-------|--------------------------------------|------------------|------------------------------|
| `--distribution`        | `-d`  | `spec.cluster.distribution`          | `Kind`           | `init`                       |
| `--distribution-config` | –     | `spec.cluster.distributionConfig`    | `kind.yaml`      | `init`                       |
| `--context`             | `-c`  | `spec.cluster.connection.context`    | Auto-derived     | `init`                       |
| `--kubeconfig`          | `-k`  | `spec.cluster.connection.kubeconfig` | `~/.kube/config` | `init`                       |
| `--source-directory`    | `-s`  | `spec.workload.sourceDirectory`      | `k8s`            | `init`                       |
| `--cni`                 | –     | `spec.cluster.cni`                   | `Default`        | `init`                       |
| `--csi`                 | –     | `spec.cluster.csi`                   | `Default`        | `init` (not yet implemented) |
| `--metrics-server`      | –     | `spec.cluster.metricsServer`         | `Enabled`        | `init`, `create`             |
| `--cert-manager`        | –     | `spec.cluster.certManager`           | `Disabled`       | `init`, `create`             |
| `--local-registry`      | –     | `spec.cluster.localRegistry`         | `Disabled`       | `init`                       |
| `--local-registry-port` | –     | (port configuration)                 | `5111`           | `init`                       |
| `--gitops-engine`       | `-g`  | `spec.cluster.gitOpsEngine`          | `None`           | `init`                       |
| `--mirror-registry`     | –     | (multiple allowed)                   | None             | `init`                       |

> Environment variables follow the pattern `KSAIL_SPEC_<FIELD>` where field names are uppercase with underscores.

### Command Examples

**Initialize a new project:**

```bash
ksail cluster init --distribution Kind --cni Cilium --metrics-server Enabled
```

**Create cluster with overrides:**

```bash
ksail cluster create --metrics-server Disabled
```

**Enable local registry:**

```bash
ksail cluster init --local-registry Enabled --local-registry-port 5111
```

**Configure mirror registries:**

```bash
ksail cluster init \
  --mirror-registry docker.io=https://registry-1.docker.io \
  --mirror-registry gcr.io=https://gcr.io
```

**Enable GitOps engine:**

```bash
ksail cluster init --gitops-engine Flux --local-registry Enabled
```

### Workload Commands

KSail provides commands for managing workloads through the `ksail workload` subcommand family:

**Manifest management:**

- `ksail workload apply` - Apply manifests using kubectl or kustomize
- `ksail workload validate` - Validate manifests with kubeconform
- `ksail workload gen` - Generate Kubernetes resource templates

**GitOps workflow:**

- `ksail workload push` - Package and push manifests as OCI artifact to local registry
- `ksail workload reconcile` - Trigger GitOps reconciliation and wait for completion

**Push command flags:**

| Flag         | Purpose                                        |
|--------------|------------------------------------------------|
| `--validate` | Validate manifests before pushing to registry. |

The `push` command validates manifests when either:

1. The `--validate` flag is set
2. `spec.workload.validateOnPush` is `true` in `ksail.yaml`

**Kubectl wrappers:**

- `ksail workload get` - Get resources
- `ksail workload edit` - Edit resources
- `ksail workload logs` - View container logs
- `ksail workload exec` - Execute commands in containers
- `ksail workload wait` - Wait for resource conditions

**Reconcile command flags:**

| Flag        | Purpose                                                     |
|-------------|-------------------------------------------------------------|
| `--timeout` | Timeout for reconciliation (e.g., `10m`). Overrides config. |

The `reconcile` command respects timeout in this order:

1. `--timeout` flag if provided
2. `spec.cluster.connection.timeout` from `ksail.yaml`
3. Default 5-minute timeout

**Example usage:**

```bash
# Push manifests to local registry
ksail workload push

# Push and validate manifests
ksail workload push --validate

# Trigger reconciliation with default timeout
ksail workload reconcile

# Override timeout
ksail workload reconcile --timeout 10m
```

## When to Use What

- **CLI flags** – Temporary overrides during development
- **`ksail.yaml`** – Project-wide defaults
- **Distribution configs** – Distribution-specific settings (node counts, port mappings)
- **Environment variables** – CI/CD or machine-specific overrides

## Editor Configuration

KSail uses a configured editor for:

- `ksail cipher edit` – Edit encrypted secrets with SOPS
- `ksail workload edit` – Edit Kubernetes resources
- `ksail cluster connect` – Edit actions in k9s

### Editor Resolution (highest to lowest)

1. `--editor` flag
2. `spec.editor` in `ksail.yaml`
3. Environment variables (`SOPS_EDITOR`, `KUBE_EDITOR`, `EDITOR`, `VISUAL`)
4. Fallback (`vim`, `nano`, `vi`)

### Examples

**Via `ksail.yaml`:**

```yaml
spec:
  editor: code --wait
```

**Via CLI:**

```bash
ksail cipher edit --editor "code --wait" secrets.yaml
ksail workload edit --editor vim deployment/my-app
```

**Via environment:**

```bash
export EDITOR="code --wait"
ksail cipher edit secrets.yaml
```
