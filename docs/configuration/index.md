---
title: "Configuration"
nav_order: 3
has_children: true
---

# Configuration

KSail supports multiple configuration sources, enabling flexibility from quick prototyping to production-ready GitOps workflows.

## How Configuration Works

KSail configuration controls every aspect of cluster creation and workload management—from the Kubernetes distribution to component installation, networking, and GitOps settings.

Configuration can come from multiple sources, allowing you to:

- **Commit defaults** in `ksail.yaml` for team consistency
- **Override temporarily** with CLI flags for testing
- **Customize per-environment** with environment variables
- **Leverage distribution features** through native config files

## Configuration Sources

KSail accepts configuration from four sources:

| Source                                         | Description                                   | Use Case                           |
|------------------------------------------------|-----------------------------------------------|------------------------------------|
| **[ksail.yaml](declarative-configuration.md)** | Declarative YAML file                         | Project defaults, version control  |
| **[CLI Flags](cli-flags/root.md)**             | Command-line arguments                        | Temporary overrides, CI/CD         |
| **Environment Variables**                      | `KSAIL_`-prefixed variables                   | Machine-specific settings, secrets |
| **Distribution Config**                        | `kind.yaml`, `k3d.yaml`, `talos/`, `eks.yaml` | Distribution-specific features     |

## Configuration Precedence

When the same setting is specified in multiple sources, KSail uses this order (highest to lowest priority):

```text
1. CLI flags          (e.g., --metrics-server Disabled)
2. Environment vars   (e.g., KSAIL_SPEC_CLUSTER_DISTRIBUTION=K3d)
3. ksail.yaml         (nearest file in current or parent directories)
4. Built-in defaults
```

**Example:** If `ksail.yaml` sets `distribution: Kind` but you run `ksail cluster create --distribution K3d`, the CLI flag wins and K3d is used.

This precedence model means you can commit sensible defaults while still allowing temporary overrides without editing files.

## When to Use What

### ksail.yaml

Use declarative configuration for:

- **Project-wide defaults** that should be version-controlled
- **Team consistency** so everyone uses the same settings
- **Cluster identity** (distribution, CNI, GitOps engine)
- **Reproducible environments** across machines

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
    cni: Cilium
    gitOpsEngine: Flux
```

### CLI Flags

Use command-line flags for:

- **Temporary overrides** during development
- **Testing different configurations** without editing files
- **CI/CD pipelines** with environment-specific settings
- **Quick experiments** before committing changes

```bash
# Test with K3d instead of Kind (without changing ksail.yaml)
ksail cluster create --distribution K3d

# Disable metrics server for a lightweight test
ksail cluster create --metrics-server Disabled
```

### Environment Variables

Use environment variables for:

- **Machine-specific settings** (different kubeconfig paths)
- **CI/CD secrets** and credentials
- **Global preferences** that apply across all projects

```bash
# Set distribution for all ksail commands in this shell
export KSAIL_SPEC_CLUSTER_DISTRIBUTION=K3d
ksail cluster create
```

Environment variables use the `KSAIL_` prefix and follow the configuration path in uppercase with underscores:

| Setting                     | Environment Variable              |
|-----------------------------|-----------------------------------|
| `spec.cluster.distribution` | `KSAIL_SPEC_CLUSTER_DISTRIBUTION` |
| `spec.cluster.cni`          | `KSAIL_SPEC_CLUSTER_CNI`          |
| `spec.cluster.gitOpsEngine` | `KSAIL_SPEC_CLUSTER_GITOPSENGINE` |

### Distribution Config

Use distribution configuration files for:

- **Distribution-specific features** not exposed in `ksail.yaml`
- **Advanced customization** (kernel parameters, extra mounts)
- **Node topology** (control-plane/worker counts, port mappings)

These files follow the native schema for each distribution:

- **Kind:** `kind.yaml` – [Kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
- **K3d:** `k3d.yaml` – [K3d Configuration](https://k3d.io/stable/usage/configfile/)
- **Talos:** `talos/` – [Talos Machine Config](https://www.talos.dev/latest/reference/configuration/)

## Next Steps

- **[Declarative Configuration](declarative-configuration.md)** – Complete `ksail.yaml` reference
- **[CLI Flags](cli-flags/root.md)** – All command-line flags by command
