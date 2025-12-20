---
title: "Configuration"
nav_order: 3
---

# Configuration

KSail keeps cluster configuration reproducible through declarative YAML files and CLI overrides. Run `ksail cluster init` to generate configuration files that the team can commit and share.

## Configuration Precedence

Configuration is resolved in the following order (highest precedence first):

1. CLI flags supplied on the command (e.g., `--metrics-server Disabled`)
2. Environment variables prefixed with `KSAIL_` (e.g., `KSAIL_SPEC_DISTRIBUTION=K3d`)
3. The nearest `ksail.yaml` in the current or parent directories
4. Built-in defaults

## Declarative Configuration

Every KSail project includes a `ksail.yaml` file that describes the desired cluster. The CLI reads this file on every invocation and validates it before taking action.

### Example ksail.yaml

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  distribution: Kind
  distributionConfig: kind.yaml
  sourceDirectory: k8s
  editor: code --wait
  connection:
    kubeconfig: ~/.kube/config
    context: kind-local
  cni: Default
  metricsServer: Enabled
  certManager: Disabled
  localRegistry: Disabled
  gitOpsEngine: None
```

### Configuration Fields

| Field                   | Type     | Default              | Values                         | Description                                                     |
|-------------------------|----------|----------------------|--------------------------------|-----------------------------------------------------------------|
| `distribution`          | enum     | `Kind`               | `Kind`, `K3d`                  | Kubernetes distribution to use.                                 |
| `distributionConfig`    | string   | `kind.yaml`          | File path                      | Path to distribution-specific YAML (`kind.yaml` or `k3d.yaml`). |
| `sourceDirectory`       | string   | `k8s`                | Directory path                 | Location of workload manifests.                                 |
| `editor`                | string   | –                    | Command with args              | Editor for interactive workflows (e.g. `code --wait`, `vim`).   |
| `connection.kubeconfig` | string   | `~/.kube/config`     | File path                      | Path to kubeconfig file.                                        |
| `connection.context`    | string   | Derived from cluster | kubeconfig context             | Context name (Kind: `kind-<name>`, K3d: `k3d-<name>`).          |
| `connection.timeout`    | duration | –                    | Go duration (e.g. `30s`, `5m`) | Optional timeout for cluster operations.                        |
| `cni`                   | enum     | `Default`            | `Default`, `Cilium`, `None`    | Container Network Interface to install.                         |
| `csi`                   | enum     | `Default`            | `Default`, `LocalPathStorage`  | Container Storage Interface (not yet implemented).              |
| `metricsServer`         | enum     | `Enabled`            | `Enabled`, `Disabled`          | Install metrics-server for resource metrics.                    |
| `certManager`           | enum     | `Disabled`           | `Enabled`, `Disabled`          | Install cert-manager for TLS certificates.                      |
| `localRegistry`         | enum     | `Disabled`           | `Enabled`, `Disabled`          | Provision local OCI registry.                                   |
| `gitOpsEngine`          | enum     | `None`               | `None`, `Flux`, `ArgoCD`       | GitOps engine to install.                                       |

> The CLI applies defaults for any omitted field. For example, if `cni` is not present, KSail uses `Default`, which uses the distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d).

### Distribution Configs

KSail stores distribution configuration alongside `ksail.yaml`:

- **`kind.yaml`** – [Kind configuration](https://kind.sigs.k8s.io/docs/user/configuration/) for node layout, networking, and port mappings
- **`k3d.yaml`** – [K3d configuration](https://k3d.io/stable/usage/configfile/) for K3s-specific options

Use the `spec.distributionConfig` field to point to the desired file.

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

| Flag                    | Short | Config Field                 | Default          | Commands                     |
|-------------------------|-------|------------------------------|------------------|------------------------------|
| `--distribution`        | `-d`  | `spec.distribution`          | `Kind`           | `init`                       |
| `--distribution-config` | –     | `spec.distributionConfig`    | `kind.yaml`      | `init`                       |
| `--context`             | `-c`  | `spec.connection.context`    | Auto-derived     | `init`                       |
| `--kubeconfig`          | `-k`  | `spec.connection.kubeconfig` | `~/.kube/config` | `init`                       |
| `--source-directory`    | `-s`  | `spec.sourceDirectory`       | `k8s`            | `init`                       |
| `--cni`                 | –     | `spec.cni`                   | `Default`        | `init`                       |
| `--csi`                 | –     | `spec.csi`                   | `Default`        | `init` (not yet implemented) |
| `--metrics-server`      | –     | `spec.metricsServer`         | `Enabled`        | `init`, `create`             |
| `--cert-manager`        | –     | `spec.certManager`           | `Disabled`       | `init`, `create`             |
| `--local-registry`      | –     | `spec.localRegistry`         | `Disabled`       | `init`                       |
| `--local-registry-port` | –     | (port configuration)         | `5111`           | `init`                       |
| `--gitops-engine`       | `-g`  | `spec.gitOpsEngine`          | `None`           | `init`                       |
| `--mirror-registry`     | –     | (multiple allowed)           | None             | `init`                       |

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

## When to Use What

- **Use CLI flags** for temporary overrides during development
- **Edit `ksail.yaml`** for project-wide defaults that all commands will use
- **Edit `kind.yaml` or `k3d.yaml`** for distribution-specific settings like node counts or port mappings
- **Use environment variables** for CI/CD pipelines or machine-specific overrides

These configuration layers allow you to balance version-controlled defaults with flexible runtime overrides.

## Editor Configuration

KSail supports configuring a default editor for interactive workflows. This editor is used by:

- `ksail cipher edit` - Edit encrypted secrets with SOPS
- `ksail workload edit` - Edit Kubernetes resources with kubectl
- `ksail cluster connect` - Editor for k9s edit actions

### Editor Resolution Order

The editor is resolved with the following precedence (highest to lowest):

1. `--editor` flag on the command
2. `spec.editor` field in `ksail.yaml`
3. Environment variables (`SOPS_EDITOR`, `KUBE_EDITOR`, `EDITOR`, `VISUAL`)
4. Fallback to available editors (`vim`, `nano`, `vi`)

### Configuration Examples

**Via ksail.yaml:**

```yaml
apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  editor: code --wait
  # ... other configuration
```

**Via CLI flag:**

```bash
# Edit a secret with VS Code
ksail cipher edit --editor "code --wait" secrets.yaml

# Edit a deployment with vim
ksail workload edit --editor vim deployment/my-app

# Connect with nano as the default editor
ksail cluster connect --editor nano
```

**Via environment variable:**

```bash
# Set for all commands
export EDITOR="code --wait"
ksail cipher edit secrets.yaml

# Or use SOPS_EDITOR for cipher commands
export SOPS_EDITOR="vim"
ksail cipher edit secrets.yaml
```

### Editor Command Format

The editor value can include command-line arguments:

- `vim` - Simple editor
- `code --wait` - VS Code with wait flag
- `emacs -nw` - Emacs in terminal mode

The command is parsed by splitting on whitespace, similar to how shell commands work.
