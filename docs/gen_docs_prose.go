// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the MIT License.

//go:build ignore

// gen_docs_prose.go contains prose constants used by gen_docs.go to build the
// configuration reference page. Separated to keep gen_docs.go focused on logic.
package main

// bt is a single backtick helper for embedding in raw strings.
const bt = "`"

// cbt is the triple-backtick code-block marker.
const cbt = bt + bt + bt

// configFrontmatter is the YAML frontmatter for the configuration reference page.
const configFrontmatter = `---
title: Declarative Configuration
description: Complete reference for ksail.yaml - the project-level configuration file that defines your cluster's desired state.
---`

// configIntroProse introduces the configuration file.
const configIntroProse = `KSail uses declarative YAML configuration files for reproducible cluster setup. This page describes ` + bt + `ksail.yaml` + bt + ` — the project-level configuration file that defines your cluster's desired state.

## What is ksail.yaml?

Each KSail project includes a ` + bt + `ksail.yaml` + bt + ` file describing the cluster and workload configuration. Run ` + bt + `ksail cluster init` + bt + ` to generate this file, which can be committed to version control and shared with your team.

The configuration file uses the ` + bt + `ksail.io/v1alpha1` + bt + ` API version and follows the ` + bt + `Cluster` + bt + ` kind schema. It defines:

- **Cluster settings**: distribution, networking, components
- **Connection details**: kubeconfig path, context, timeouts
- **Workload configuration**: manifest directory, validation preferences
- **Editor preferences**: for interactive workflows`

// configEnvVarProse documents environment variable expansion.
const configEnvVarProse = `## Environment Variable Expansion

KSail supports environment variable expansion in all string configuration values using the ` + bt + `${VAR_NAME}` + bt + ` syntax. This enables:

- **Secure credential management**: Keep sensitive values out of version control
- **Environment-specific configuration**: Use different values per environment (dev/staging/prod)
- **Dynamic path resolution**: Reference user-specific or system-specific paths

### Syntax

**Basic syntax:** ` + bt + `${VARIABLE_NAME}` + bt + ` — Reference an environment variable. If not set, expands to an empty string and logs a warning.

**Default value syntax:** ` + bt + `${VARIABLE_NAME:-default}` + bt + ` — Use a default value if the variable is not set. No warning is logged when using defaults.

` + cbt + `yaml
spec:
  editor: "${EDITOR:-vim}"
  cluster:
    connection:
      kubeconfig: "${HOME}/.kube/config"
      context: "${KUBE_CONTEXT:-kind-kind}"
    distributionConfig: "${CONFIG_DIR:-configs}/kind.yaml"
    localRegistry:
      registry: "${REGISTRY:-localhost:5000}"
    vanilla:
      mirrorsDir: "${MIRRORS_DIR:-mirrors}"
    talos:
      config: "${TALOS_CONFIG_PATH:-~/.talos/config}"
    hetzner:
      sshKeyName: "${HCLOUD_SSH_KEY}"
  workload:
    sourceDirectory: "${WORKLOAD_DIR:-k8s}"
  chat:
    model: "${CHAT_MODEL:-gpt-4o}"
` + cbt + `

### Expansion Behavior

| Syntax            | Variable Set | Variable Not Set          |
| ----------------- | ------------ | ------------------------- |
| ` + bt + `${VAR}` + bt + `          | Uses value   | Empty string + warning    |
| ` + bt + `${VAR:-default}` + bt + ` | Uses value   | Uses default (no warning) |
| ` + bt + `${VAR:-}` + bt + `        | Uses value   | Empty string (no warning) |

### Scope

Environment variables are expanded in:

1. **ksail.yaml** — All supported string fields (see below)
2. **Distribution configs** — ` + bt + `kind.yaml` + bt + `, ` + bt + `k3d.yaml` + bt + ` file contents
3. **Talos patches** — All YAML patch files in ` + bt + `talos/cluster/` + bt + `, ` + bt + `talos/control-planes/` + bt + `, ` + bt + `talos/workers/` + bt + `

This means you can use ` + bt + `${VAR}` + bt + ` syntax inside your Kind, K3d, or Talos configuration files too:

` + cbt + `yaml
# kind.yaml - Environment variables are expanded before parsing
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${REGISTRY:-localhost:5000}"]
      endpoint = ["http://${REGISTRY:-localhost:5000}"]
` + cbt + `

` + cbt + `yaml
# talos/cluster/registry.yaml - Environment variables are expanded
machine:
  registries:
    mirrors:
      docker.io:
        endpoints:
          - http://${REGISTRY:-localhost:5000}
` + cbt + `

### Supported Fields

Environment variable expansion works in these string fields:

**General:**

- ` + bt + `spec.editor` + bt + ` - Editor command
- ` + bt + `spec.cluster.connection.kubeconfig` + bt + ` - Kubeconfig path
- ` + bt + `spec.cluster.connection.context` + bt + ` - Kubernetes context name
- ` + bt + `spec.cluster.distributionConfig` + bt + ` - Distribution config path
- ` + bt + `spec.cluster.localRegistry.registry` + bt + ` - Registry specification (including credentials)
- ` + bt + `spec.workload.sourceDirectory` + bt + ` - Workload manifest directory
- ` + bt + `spec.chat.model` + bt + ` - Chat model name

**Distribution-specific:**

- ` + bt + `spec.cluster.vanilla.mirrorsDir` + bt + ` - Mirror configuration directory
- ` + bt + `spec.cluster.talos.config` + bt + ` - Talos configuration path

**Provider-specific:**

- ` + bt + `spec.cluster.hetzner.sshKeyName` + bt + ` - SSH key name
- ` + bt + `spec.cluster.hetzner.networkName` + bt + ` - Network name
- ` + bt + `spec.cluster.hetzner.placementGroup` + bt + ` - Placement group name

### Example: Credentials

` + cbt + `yaml
spec:
  cluster:
    localRegistry:
      registry: "${REGISTRY_USER}:${REGISTRY_PASS}@${REGISTRY_HOST:-ghcr.io}/myorg/myrepo"
` + cbt + `

` + cbt + `bash
export REGISTRY_USER="github-user"
export REGISTRY_PASS="ghp_secrettoken123"
ksail cluster create
` + cbt + `

### Example: Multi-Environment Setup

` + cbt + `yaml
spec:
  cluster:
    connection:
      context: "${CLUSTER_NAME:-kind-kind}"
    distributionConfig: "${ENV:-dev}/kind.yaml"
  workload:
    sourceDirectory: "${ENV:-dev}/k8s"
` + cbt + `

` + cbt + `bash
# Development (using defaults)
ksail cluster create

# Production (override with environment variables)
export ENV="prod"
export CLUSTER_NAME="prod-cluster"
ksail cluster create
` + cbt + `

### Example: Distribution Config with Variables

Environment variables also work inside distribution configuration files:

` + cbt + `yaml
# kind.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: ${INGRESS_PORT:-80}
        hostPort: ${HOST_PORT:-8080}
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${MIRROR_REGISTRY:-docker.io}"]
      endpoint = ["http://${REGISTRY:-localhost:5000}"]
` + cbt + `

` + cbt + `yaml
# talos/cluster/registry.yaml
machine:
  registries:
    mirrors:
      docker.io:
        endpoints:
          - http://${REGISTRY:-localhost:5000}
` + cbt + ``

// configMinimalExampleProse has the minimal ksail.yaml example.
const configMinimalExampleProse = `## Minimal Example

` + cbt + `yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
` + cbt + `

This minimal configuration creates a Vanilla cluster (implemented with Kind) using defaults for all other settings.`

// configCompleteExampleProse has the complete ksail.yaml example.
const configCompleteExampleProse = `## Complete Example

` + cbt + `yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  editor: code --wait
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    connection:
      kubeconfig: ~/.kube/config
      context: kind-kind
      timeout: 5m
    cni: Cilium
    csi: Default
    metricsServer: Enabled
    certManager: Enabled
    policyEngine: Kyverno
    localRegistry:
      registry: localhost:5050
    gitOpsEngine: Flux
  workload:
    sourceDirectory: k8s
    validateOnPush: true
` + cbt + ``

// distributionDetails provides prose after the distribution enum list.
const distributionDetails = `See [Distributions](/concepts/#distributions) for detailed information about each distribution.

- ` + bt + `Vanilla` + bt + ` – Standard upstream Kubernetes (implemented with [Kind](https://kind.sigs.k8s.io/))
- ` + bt + `K3s` + bt + ` – Lightweight Kubernetes (implemented with [K3d](https://k3d.io/))
- ` + bt + `Talos` + bt + ` – [Talos Linux](https://www.talos.dev/) in Docker containers or Hetzner Cloud servers`

// providerDetails provides prose after the provider enum list.
const providerDetails = `See [Providers](/concepts/#providers) for more details.

- ` + bt + `Docker` + bt + ` – Run nodes as Docker containers (local development)
- ` + bt + `Hetzner` + bt + ` – Run nodes on Hetzner Cloud servers (requires ` + bt + `HCLOUD_TOKEN` + bt + `)

> [!NOTE]
> Hetzner provider is only supported with the ` + bt + `Talos` + bt + ` distribution.`

// configDistributionProse describes the distributionConfig field.
const configDistributionProse = `#### distributionConfig

Path to the distribution-specific configuration file or directory. This tells KSail where to find settings like node counts, port mappings, and distribution-specific features.

**Default values by distribution:**

- ` + bt + `Vanilla` + bt + ` → ` + bt + `kind.yaml` + bt + `
- ` + bt + `K3s` + bt + ` → ` + bt + `k3d.yaml` + bt + `
- ` + bt + `Talos` + bt + ` → ` + bt + `talos/` + bt + ` (directory)

See [Distribution Configuration](#distribution-configuration) below for details on each format.`

// configConnectionProse describes the connection sub-object.
const configConnectionProse = `#### connection (Connection)

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| ` + bt + `kubeconfig` + bt + ` | string | ` + bt + `~/.kube/config` + bt + ` | Path to kubeconfig file |
| ` + bt + `context` + bt + ` | string | (derived) | Kubeconfig context name |
| ` + bt + `timeout` + bt + ` | duration | – | Timeout for cluster operations |

**Context defaults by distribution:**

- ` + bt + `Vanilla` + bt + ` → ` + bt + `kind-kind` + bt + `
- ` + bt + `K3s` + bt + ` → ` + bt + `k3d-k3d-default` + bt + `
- ` + bt + `Talos` + bt + ` → ` + bt + `admin@talos-default` + bt + `

**Timeout format:** Go duration string (e.g., ` + bt + `30s` + bt + `, ` + bt + `5m` + bt + `, ` + bt + `1h` + bt + `)`

// cniDetails provides prose after the CNI enum list.
const cniDetails = `See [CNI](/concepts/#container-network-interface-cni) for more details.

- ` + bt + `Default` + bt + ` – Uses the distribution's built-in CNI (` + bt + `kindnetd` + bt + ` for Vanilla, ` + bt + `flannel` + bt + ` for K3s)
- ` + bt + `Cilium` + bt + ` – Installs [Cilium](https://cilium.io/) for advanced networking and observability
- ` + bt + `Calico` + bt + ` – Installs [Calico](https://www.tigera.io/project-calico/) for network policies`

// csiDetails provides prose after the CSI enum list.
const csiDetails = `See [CSI](/concepts/#container-storage-interface-csi) for more details.

- ` + bt + `Default` + bt + ` – Uses the distribution × provider's default behavior:
  - K3s: includes local-path-provisioner
  - Vanilla/Talos × Docker: no CSI
  - Talos × Hetzner: includes Hetzner CSI driver
- ` + bt + `Enabled` + bt + ` – Explicitly installs CSI driver (local-path-provisioner for local clusters, Hetzner CSI for Talos × Hetzner)
- ` + bt + `Disabled` + bt + ` – Disables CSI installation (for K3s, this disables the default local-storage)`

// metricsServerDetails provides prose after the MetricsServer enum list.
const metricsServerDetails = `Whether to install [metrics-server](/concepts/#metrics-server) for resource metrics.

- ` + bt + `Default` + bt + ` – Uses distribution's default behavior (K3s includes metrics-server; Vanilla and Talos do not)
- ` + bt + `Enabled` + bt + ` – Install metrics-server
- ` + bt + `Disabled` + bt + ` – Skip installation

When metrics-server is enabled on Vanilla or Talos, KSail automatically:

1. Configures kubelet certificate rotation (` + bt + `serverTLSBootstrap: true` + bt + `)
2. Installs [kubelet-csr-approver](/concepts/#kubelet-csr-approver) to approve certificate requests
3. Deploys metrics-server with secure TLS communication

> [!NOTE]
> K3s includes metrics-server by default, so this setting has no effect on K3s.`

// certManagerDetails provides prose after the CertManager enum list.
const certManagerDetails = `Whether to install [cert-manager](/concepts/#cert-manager) for TLS certificate management.`

// policyEngineDetails provides prose after the PolicyEngine enum list.
const policyEngineDetails = `Policy engine to install for enforcing security, compliance, and best practices. See [Policy Engines](/concepts/#policy-engines) for more details about Kyverno and Gatekeeper.`

// configLocalRegistryProse describes the localRegistry sub-object.
const configLocalRegistryProse = `#### localRegistry

Registry configuration for GitOps workflows. Supports local Docker registries or external registries with authentication.

**Format:** ` + bt + `[user:pass@]host[:port][/path]` + bt + `

**Examples:**

- ` + bt + `localhost:5050` + bt + ` – Local Docker registry
- ` + bt + `ghcr.io/myorg/myrepo` + bt + ` – GitHub Container Registry
- ` + bt + `${USER}:${PASS}@ghcr.io:443/myorg` + bt + ` – With credentials from environment variables

> [!NOTE]
> Credentials support ` + bt + `${ENV_VAR}` + bt + ` placeholders for secure handling.`

// gitOpsEngineDetails provides prose after the GitOpsEngine enum list.
const gitOpsEngineDetails = `GitOps engine to install for continuous deployment workflows. See [GitOps](/concepts/#gitops) for more details about Flux and ArgoCD. When set to ` + bt + `Flux` + bt + ` or ` + bt + `ArgoCD` + bt + `, KSail scaffolds a GitOps CR (FluxInstance or ArgoCD Application) into your source directory.

- ` + bt + `None` + bt + ` – No GitOps engine
- ` + bt + `Flux` + bt + ` – Install [Flux CD](https://fluxcd.io/) and scaffold FluxInstance CR
- ` + bt + `ArgoCD` + bt + ` – Install [Argo CD](https://argo-cd.readthedocs.io/) and scaffold Application CR`

// configDistToolOptions describes distribution and tool-specific options.
const configDistToolOptions = `#### Distribution and Tool Options

Advanced configuration options are direct fields under ` + bt + `spec.cluster` + bt + `. See [Schema Support](#schema-support) for the complete structure.

**Talos options (` + bt + `spec.cluster.talos` + bt + `):**

- ` + bt + `controlPlanes` + bt + ` – Number of control-plane nodes (default: ` + bt + `1` + bt + `)
- ` + bt + `workers` + bt + ` – Number of worker nodes (default: ` + bt + `0` + bt + `)
- ` + bt + `config` + bt + ` – Path to talosconfig file (default: ` + bt + `~/.talos/config` + bt + `)
- ` + bt + `iso` + bt + ` – Cloud provider ISO/image ID for Talos Linux (default: ` + bt + `122630` + bt + ` for x86)

**Hetzner options (` + bt + `spec.cluster.hetzner` + bt + `):**

- ` + bt + `controlPlaneServerType` + bt + ` – Server type for control-plane nodes (default: ` + bt + `cx23` + bt + `)
- ` + bt + `workerServerType` + bt + ` – Server type for worker nodes (default: ` + bt + `cx23` + bt + `)
- ` + bt + `location` + bt + ` – Datacenter location: ` + bt + `fsn1` + bt + `, ` + bt + `nbg1` + bt + `, ` + bt + `hel1` + bt + ` (default: ` + bt + `fsn1` + bt + `)
- ` + bt + `networkName` + bt + ` – Private network name (default: ` + bt + `<cluster>-network` + bt + `)
- ` + bt + `networkCidr` + bt + ` – Network CIDR block (default: ` + bt + `10.0.0.0/16` + bt + `)
- ` + bt + `sshKeyName` + bt + ` – SSH key name for server access (optional)
- ` + bt + `tokenEnvVar` + bt + ` – Environment variable for API token (default: ` + bt + `HCLOUD_TOKEN` + bt + `)

**Vanilla options (` + bt + `spec.cluster.vanilla` + bt + `):**

- ` + bt + `mirrorsDir` + bt + ` – Directory for containerd host mirror configuration`

// configDistributionConfigProse describes distribution configuration files.
const configDistributionConfigProse = `## Distribution Configuration

KSail references distribution-specific configuration files to customize cluster behavior. The path to these files is set via ` + bt + `spec.cluster.distributionConfig` + bt + `.

### Vanilla (implemented with Kind) Configuration

**Default:** ` + bt + `kind.yaml` + bt + `

The Vanilla distribution is configured via a YAML file following the Kind configuration schema. This allows you to customize:

- Node images and versions
- Extra port mappings
- Extra mounts
- Networking settings

**Documentation:** [Kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)

**Example:**

` + cbt + `yaml
# kind.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000
        hostPort: 30000
` + cbt + `

### K3s (implemented with K3d) Configuration

**Default:** ` + bt + `k3d.yaml` + bt + `

The K3s distribution is configured via a YAML file following the K3d configuration schema. This allows you to customize:

- Server and agent counts
- Port mappings
- Volume mounts
- Registry configurations

**Documentation:** [K3d Configuration](https://k3d.io/stable/usage/configfile/)

**Example:**

` + cbt + `yaml
# k3d.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
servers: 1
agents: 2
ports:
  - port: 8080:80
    nodeFilters:
      - loadbalancer
` + cbt + `

### Talos Configuration

**Default:** ` + bt + `talos/` + bt + ` directory

Talos uses a directory structure for [Talos machine configuration patches](https://www.talos.dev/latest/reference/configuration/). Each directory contains YAML patch files that modify the Talos machine configuration.

**Documentation:** [Talos Configuration Reference](https://www.talos.dev/latest/reference/configuration/)

**Directory structure and examples:**

` + cbt + `yaml
# talos/cluster/kubelet.yaml
machine:
  kubelet:
    extraArgs:
      max-pods: "250"
` + cbt + `

` + cbt + `yaml
# talos/control-planes/api.yaml
machine:
  kubelet:
    extraArgs:
      feature-gates: "EphemeralContainers=true"
` + cbt + `

` + cbt + `yaml
# talos/workers/custom.yaml
machine:
  sysctls:
    net.core.somaxconn: "65535"
` + cbt + `

Use ` + bt + `spec.cluster.talos` + bt + ` to configure node counts:

` + cbt + `yaml
spec:
  cluster:
    distribution: Talos
    distributionConfig: talos
    talos:
      controlPlanes: 3
      workers: 2
` + cbt + ``

// configSchemaProse describes JSON Schema support.
const configSchemaProse = `## Schema Support

KSail provides a JSON Schema for IDE validation and autocompletion. Reference it at the top of your ` + bt + `ksail.yaml` + bt + `:

` + cbt + `yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  # ...
` + cbt + `

IDEs with YAML language support (like VS Code with the Red Hat YAML extension) will provide:

- Field autocompletion
- Inline documentation
- Validation errors for invalid values
- Enum suggestions for fields like ` + bt + `distribution` + bt + `, ` + bt + `cni` + bt + `, etc.`
