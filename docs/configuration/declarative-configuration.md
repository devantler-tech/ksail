---
title: "Declarative Configuration"
parent: Configuration
---

# Declarative Configuration

KSail uses declarative YAML configuration files for reproducible cluster setup. This page describes `ksail.yaml` — the project-level configuration file that defines your cluster's desired state.

## What is ksail.yaml?

Each KSail project includes a `ksail.yaml` file describing the cluster and workload configuration. Run `ksail cluster init` to generate this file, which can be committed to version control and shared with your team.

The configuration file uses the `ksail.io/v1alpha1` API version and follows the `Cluster` kind schema. It defines:

- **Cluster settings**: distribution, networking, components
- **Connection details**: kubeconfig path, context, timeouts
- **Workload configuration**: manifest directory, validation preferences
- **Editor preferences**: for interactive workflows

## Minimal Example

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Kind
    distributionConfig: kind.yaml
```

This minimal configuration creates a Kind cluster using defaults for all other settings.

## Complete Example

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  editor: code --wait
  cluster:
    distribution: Kind
    distributionConfig: kind.yaml
    connection:
      kubeconfig: ~/.kube/config
      context: kind-kind
      timeout: 5m
    cni: Cilium
    # CSI "Default" uses the distribution's built-in storage behavior:
    # - K3d: includes local-path-provisioner by default
    # - Kind and Talos: no CSI installed; use LocalPathStorage if needed
    csi: Default
    metricsServer: Enabled
    certManager: Enabled
    policyEngine: Kyverno
    localRegistry: Enabled
    gitOpsEngine: Flux
    localRegistryOptions:
      hostPort: 5111
  workload:
    sourceDirectory: k8s
    validateOnPush: true
```

## Configuration Reference

### Top-Level Fields

| Field        | Type   | Required | Description                                    |
| ------------ | ------ | -------- | ---------------------------------------------- |
| `apiVersion` | string | Yes      | Must be `ksail.io/v1alpha1`                    |
| `kind`       | string | Yes      | Must be `Cluster`                              |
| `spec`       | object | Yes      | Cluster and workload specification (see below) |

### spec

The `spec` field is a `Spec` object that defines editor, cluster, and workload configuration.

| Field      | Type         | Default | Description                                      |
| ---------- | ------------ | ------- | ------------------------------------------------ |
| `editor`   | string       | –       | Editor command for interactive workflows         |
| `cluster`  | ClusterSpec  | –       | Cluster configuration (distribution, components) |
| `workload` | WorkloadSpec | –       | Workload manifest configuration                  |

### spec.editor

Editor command used by KSail for interactive workflows like `ksail cipher edit` or `ksail workload edit`.

**Examples:** `code --wait`, `vim`, `nano`

If not specified, KSail falls back to standard editor environment variables (`SOPS_EDITOR`, `KUBE_EDITOR`, `EDITOR`, `VISUAL`) or system defaults (`vim`, `nano`, `vi`).

### spec.cluster (ClusterSpec)

| Field                  | Type       | Default     | Description                                 |
| ---------------------- | ---------- | ----------- | ------------------------------------------- |
| `distribution`         | enum       | `Kind`      | Kubernetes distribution to use              |
| `distributionConfig`   | string     | (see below) | Path to distribution-specific configuration |
| `connection`           | Connection | –           | Cluster connection settings                 |
| `cni`                  | enum       | `Default`   | Container Network Interface                 |
| `csi`                  | enum       | `Default`   | Container Storage Interface                 |
| `metricsServer`        | enum       | `Default`   | Install metrics-server                      |
| `certManager`          | enum       | `Disabled`  | Install cert-manager                        |
| `policyEngine`         | enum       | `None`      | Policy engine to install                    |
| `localRegistry`        | enum       | `Disabled`  | Provision local OCI registry                |
| `gitOpsEngine`         | enum       | `None`      | GitOps engine to install                    |
| `localRegistryOptions` | object     | –           | Local registry configuration options        |
| `kind`                 | object     | –           | Kind-specific options                       |
| `k3d`                  | object     | –           | K3d-specific options                        |
| `talos`                | object     | –           | Talos-specific options                      |
| `cilium`               | object     | –           | Cilium CNI options                          |
| `calico`               | object     | –           | Calico CNI options                          |
| `flux`                 | object     | –           | Flux GitOps options                         |
| `argocd`               | object     | –           | ArgoCD GitOps options                       |
| `helm`                 | object     | –           | Helm tool options (reserved)                |
| `kustomize`            | object     | –           | Kustomize tool options (reserved)           |

#### distribution

Kubernetes distribution to use for the local cluster. See [Distributions](../concepts.md#distributions) for detailed information about each distribution.

**Valid values:**

- `Kind` (default) – Uses [Kind](https://kind.sigs.k8s.io/) to run Kubernetes in Docker
- `K3d` – Uses [K3d](https://k3d.io/) to run lightweight K3s in Docker
- `Talos` – Uses [Talos Linux](https://www.talos.dev/) in Docker containers

#### distributionConfig

Path to the distribution-specific configuration file or directory. This tells KSail where to find settings like node counts, port mappings, and distribution-specific features.

**Default values by distribution:**

- `Kind` → `kind.yaml`
- `K3d` → `k3d.yaml`
- `Talos` → `talos/` (directory)

See [Distribution Configuration](#distribution-configuration) below for details on each format.

#### connection (Connection)

| Field        | Type     | Default          | Description                    |
| ------------ | -------- | ---------------- | ------------------------------ |
| `kubeconfig` | string   | `~/.kube/config` | Path to kubeconfig file        |
| `context`    | string   | (derived)        | Kubeconfig context name        |
| `timeout`    | duration | –                | Timeout for cluster operations |

**Context defaults by distribution:**

- `Kind` → `kind-kind`
- `K3d` → `k3d-k3d-default`
- `Talos` → `admin@talos-default`

**Timeout format:** Go duration string (e.g., `30s`, `5m`, `1h`)

#### cni

Container Network Interface to install. See [CNI](../concepts.md#container-network-interface-cni) for more details.

**Valid values:**

- `Default` – Uses the distribution's built-in CNI (`kindnetd` for Kind, `flannel` for K3d)
- `Cilium` – Installs [Cilium](https://cilium.io/) for advanced networking and observability
- `Calico` – Installs [Calico](https://www.tigera.io/project-calico/) for network policies

#### csi

Container Storage Interface to install. See [CSI](../concepts.md#container-storage-interface-csi) for more details.

**Valid values:**

- `Default` – Uses the distribution's built-in storage (K3d includes local-path-provisioner; Kind does not)
- `LocalPathStorage` – Explicitly installs [local-path-provisioner](https://github.com/rancher/local-path-provisioner)

#### metricsServer

Whether to install [metrics-server](../concepts.md#metrics-server) for resource metrics.

**Valid values:**

- `Default` (default) – Uses distribution's default behavior (K3d includes metrics-server; Kind and Talos do not)
- `Enabled` – Install metrics-server
- `Disabled` – Skip installation

When metrics-server is enabled on Kind or Talos, KSail automatically:

1. Configures kubelet certificate rotation (`serverTLSBootstrap: true`)
2. Installs [kubelet-csr-approver](../concepts.md#kubelet-csr-approver) to approve certificate requests
3. Deploys metrics-server with secure TLS communication

Note: K3d includes metrics-server by default, so this setting has no effect on K3d.

#### certManager

Whether to install [cert-manager](../concepts.md#cert-manager) for TLS certificate management.

**Valid values:**

- `Enabled` – Install cert-manager
- `Disabled` (default) – Skip installation

#### policyEngine

Policy engine to install for enforcing security, compliance, and best practices. See [Policy Engines](../concepts.md#policy-engines) for more details about Kyverno and Gatekeeper.

**Valid values:**

- `None` (default) – No policy engine
- `Kyverno` – Install [Kyverno](https://kyverno.io/) for Kubernetes-native policy management
- `Gatekeeper` – Install [OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/) for OPA-based policy enforcement

#### localRegistry

Whether to provision a local [OCI registry](../concepts.md#oci-registries) container for image storage.

**Valid values:**

- `Enabled` – Provision local registry
- `Disabled` (default) – Skip registry

See [Distribution and Tool Options](#distribution-and-tool-options) for configuration details including `hostPort` (default: `5111`).

#### gitOpsEngine

GitOps engine to install for continuous deployment workflows. See [GitOps](../concepts.md#gitops) for more details about Flux and ArgoCD. When set to `Flux` or `ArgoCD`, KSail scaffolds a GitOps CR (FluxInstance or ArgoCD Application) into your source directory at `gitops/flux/flux-instance.yaml` or `gitops/argocd/application.yaml`.

**Valid values:**

- `None` (default) – No GitOps engine
- `Flux` – Install [Flux CD](https://fluxcd.io/) and scaffold FluxInstance CR
- `ArgoCD` – Install [Argo CD](https://argo-cd.readthedocs.io/) and scaffold Application CR

#### Distribution and Tool Options

Advanced configuration options are now direct fields under `spec.cluster` instead of nested under `options`. See [Schema Support](#schema-support) for the complete structure.

**Common options:**

- `spec.cluster.localRegistryOptions.hostPort` – Host port for local registry (default: `5111`)
- `spec.cluster.talos.controlPlanes` – Number of control-plane nodes (default: `1`)
- `spec.cluster.talos.workers` – Number of worker nodes (default: `0`)
- `spec.cluster.kind.mirrorsDir` – Directory for containerd host mirror configuration

### spec.workload (WorkloadSpec)

| Field             | Type    | Default | Description                                   |
| ----------------- | ------- | ------- | --------------------------------------------- |
| `sourceDirectory` | string  | `k8s`   | Directory containing Kubernetes manifests     |
| `validateOnPush`  | boolean | `false` | Validate manifests before pushing to registry |

## Distribution Configuration

KSail references distribution-specific configuration files to customize cluster behavior. The path to these files is set via `spec.cluster.distributionConfig`.

### Kind Configuration

**Default:** `kind.yaml`

Kind clusters are configured via a YAML file following the Kind configuration schema. This allows you to customize:

- Node images and versions
- Extra port mappings
- Extra mounts
- Networking settings

**Documentation:** [Kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)

**Example:**

```yaml
# kind.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000
        hostPort: 30000
```

### K3d Configuration

**Default:** `k3d.yaml`

K3d clusters are configured via a YAML file following the K3d configuration schema. This allows you to customize:

- Server and agent counts
- Port mappings
- Volume mounts
- Registry configurations

**Documentation:** [K3d Configuration](https://k3d.io/stable/usage/configfile/)

**Example:**

```yaml
# k3d.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
servers: 1
agents: 2
ports:
  - port: 8080:80
    nodeFilters:
      - loadbalancer
```

### Talos Configuration

**Default:** `talos/` directory

Talos uses a directory structure for [Talos machine configuration patches](https://www.talos.dev/latest/reference/configuration/). Each directory contains YAML patch files that modify the Talos machine configuration.

**Documentation:** [Talos Configuration Reference](https://www.talos.dev/latest/reference/configuration/)

**Directory structure and examples:**

```yaml
# talos/cluster/kubelet.yaml
# Patches applied to all nodes
machine:
  kubelet:
    extraArgs:
      max-pods: "250"
```

```yaml
# talos/control-planes/api.yaml
# Patches for control-plane nodes only
machine:
  kubelet:
    extraArgs:
      feature-gates: "EphemeralContainers=true"
```

```yaml
# talos/workers/custom.yaml
# Patches for worker nodes only
machine:
  sysctls:
    net.core.somaxconn: "65535"
```

Use `spec.cluster.talos` to configure node counts:

```yaml
spec:
  cluster:
    distribution: Talos
    distributionConfig: talos
    talos:
      controlPlanes: 3
      workers: 2
```

## Schema Support

KSail provides a JSON Schema for IDE validation and autocompletion. Reference it at the top of your `ksail.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  # ...
```

IDEs with YAML language support (like VS Code with the Red Hat YAML extension) will provide:

- Field autocompletion
- Inline documentation
- Validation errors for invalid values
- Enum suggestions for fields like `distribution`, `cni`, etc.
