// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

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

Each KSail project includes a ` + bt + `ksail.yaml` + bt + ` file describing cluster distribution, networking, components, and workload configuration. Run ` + bt + `ksail project init` + bt + ` to generate it — commit to version control to share with your team.`

// configEnvVarProse documents environment variable expansion.
const configEnvVarProse = `## Environment Variable Expansion

KSail supports environment variable expansion in all string configuration values using the ` + bt + `${VAR_NAME}` + bt + ` syntax for secure credentials, environment-specific paths, and dynamic values.

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
  provider:
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

Environment variables are expanded in all string fields of ` + bt + `ksail.yaml` + bt + `, distribution configs (` + bt + `kind.yaml` + bt + `, ` + bt + `k3d.yaml` + bt + `), and Talos patch files (` + bt + `talos/cluster/` + bt + `, ` + bt + `talos/control-planes/` + bt + `, ` + bt + `talos/workers/` + bt + `):

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

Because patch files are expanded too, you can inject **registry credentials** into the Talos
machine config as config-as-code — keeping the secret in the environment, never committed. When
` + bt + `localRegistry.credentials` + bt + ` is configured, Talos maps placeholders for both ` + bt + `tokenEnvVar` + bt + ` and
` + bt + `clusterTokenEnvVar` + bt + ` to the effective cluster pull source. Other placeholders still resolve directly.
A configured cluster source remains authoritative even when its variable is missing or empty, so a
default expression cannot silently substitute a broader credential:

` + cbt + `yaml
# talos/cluster/registry-auth.yaml - Credentials are injected from the environment at load time
machine:
  registries:
    config:
      registry.example.com:
        auth:
          username: ${REGISTRY_USER}
          # The common placeholder maps to the effective cluster token source.
          password: ${REGISTRY_TOKEN:-forbidden-fallback}
` + cbt + `

### Example: Credentials

Credentials may be embedded directly in the registry spec, where ` + bt + `${VAR_NAME}` + bt + ` placeholders are
expanded at load time:

` + cbt + `yaml
spec:
  cluster:
    localRegistry:
      registry: "${REGISTRY_USER}:${REGISTRY_PASS}@${REGISTRY_HOST:-ghcr.io}/myorg/myrepo"
` + cbt + `

### Example: Separate push and pull credentials

To give the cluster a **least-privilege, pull-only token** while the CLI keeps a token that can also
push, declare which environment variable each execution path reads. Following the KSail ` + bt + `*EnvVar` + bt + `
convention, these fields hold the **name** of an environment variable, never a token value:

` + cbt + `yaml
spec:
  cluster:
    localRegistry:
      registry: "${REGISTRY_USER}@registry.example.com/myorg/myrepo"
      credentials:
        tokenEnvVar: REGISTRY_TOKEN
        cliTokenEnvVar: REGISTRY_PUBLISH_TOKEN
        clusterTokenEnvVar: REGISTRY_PULL_TOKEN
` + cbt + `

Resolution is deterministic and registry-agnostic — no registry host is special-cased:

- CLI and publish paths read ` + bt + `cliTokenEnvVar` + bt + `, falling back to ` + bt + `tokenEnvVar` + bt + ` only when the override is
  not configured.
- Cluster pull paths — the Flux registry Secret, Argo CD repository credentials, and Talos node
  authentication — read ` + bt + `clusterTokenEnvVar` + bt + `, falling back to ` + bt + `tokenEnvVar` + bt + ` the same way.
- A configured override stays **authoritative** even when its environment variable is missing or
  empty; KSail never silently falls back based on process-environment state.
- When no field is set, the password embedded in ` + bt + `registry` + bt + ` is used.

When ` + bt + `clusterTokenEnvVar` + bt + ` resolves to a different variable than the push path, the credential KSail
persists into the cluster is marked pull-only, so it is never reused to publish artifacts.

` + cbt + `bash
export REGISTRY_USER="registry-user"
export REGISTRY_PUBLISH_TOKEN="write-capable-token"
export REGISTRY_PULL_TOKEN="read-only-token"
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
const distributionDetails = `See [Distributions](/concepts/#distributions) for detailed information.

- ` + bt + `Vanilla` + bt + ` (default) – Standard upstream Kubernetes via [Kind](https://kind.sigs.k8s.io/)
- ` + bt + `K3s` + bt + ` – Lightweight Kubernetes via [K3d](https://k3d.io/)
- ` + bt + `Talos` + bt + ` – [Talos Linux](https://www.talos.dev/) in Docker containers or Hetzner Cloud servers
- ` + bt + `VCluster` + bt + ` – Virtual clusters via [vCluster](https://www.vcluster.com/)
- ` + bt + `KWOK` + bt + ` – Simulated clusters via [KWOK](https://kwok.sigs.k8s.io/) (control-plane only, no real workloads)
- ` + bt + `EKS` + bt + ` – Amazon Elastic Kubernetes Service via [eksctl](https://eksctl.io/) (requires AWS credentials and the ` + bt + `eksctl` + bt + ` CLI on ` + bt + `PATH` + bt + `)
- ` + bt + `GKE` + bt + ` – Google Kubernetes Engine via the native Go SDK (requires Google Cloud Application Default Credentials and a project via ` + bt + `GOOGLE_CLOUD_PROJECT` + bt + `)
- ` + bt + `AKS` + bt + ` – Azure Kubernetes Service via the native Go SDK (requires Azure credentials and a subscription via ` + bt + `AZURE_SUBSCRIPTION_ID` + bt + `)`

// providerDetails provides prose after the provider enum list.
const providerDetails = `See [Providers](/concepts/#providers) for more details.

- ` + bt + `Docker` + bt + ` (default) – Run nodes as Docker containers (local development)
- ` + bt + `Hetzner` + bt + ` – Run nodes on Hetzner Cloud servers (requires ` + bt + `HCLOUD_TOKEN` + bt + `)
- ` + bt + `Omni` + bt + ` – Manage Talos cluster nodes through [Sidero Omni](https://omni.siderolabs.com/)
- ` + bt + `AWS` + bt + ` – Manage EKS clusters on Amazon Web Services (requires standard AWS SDK credentials)
- ` + bt + `GCP` + bt + ` – Manage GKE clusters on Google Cloud (requires Application Default Credentials)
- ` + bt + `Azure` + bt + ` – Manage AKS clusters on Microsoft Azure (requires DefaultAzureCredential-compatible credentials)

> [!NOTE]
> Hetzner and Omni providers are only supported with the ` + bt + `Talos` + bt + ` distribution. The AWS provider is only supported with the ` + bt + `EKS` + bt + ` distribution, the GCP provider only with the ` + bt + `GKE` + bt + ` distribution, and the Azure provider only with the ` + bt + `AKS` + bt + ` distribution.`

// configDistributionProse describes the distributionConfig field.
const configDistributionProse = `#### distributionConfig

Path to the distribution-specific configuration file or directory. This tells KSail where to find settings like node counts, port mappings, and distribution-specific features.

**Default values by distribution:**

- ` + bt + `Vanilla` + bt + ` → ` + bt + `kind.yaml` + bt + `
- ` + bt + `K3s` + bt + ` → ` + bt + `k3d.yaml` + bt + `
- ` + bt + `Talos` + bt + ` → ` + bt + `talos/` + bt + ` (directory)
- ` + bt + `VCluster` + bt + ` → ` + bt + `vcluster.yaml` + bt + `
- ` + bt + `KWOK` + bt + ` → ` + bt + `kwok/` + bt + ` (directory)
- ` + bt + `EKS` + bt + ` → ` + bt + `eks.yaml` + bt + `
- ` + bt + `GKE` + bt + ` → ` + bt + `gke.yaml` + bt + ` (optional – the GKE API owns the cluster shape)
- ` + bt + `AKS` + bt + ` → ` + bt + `aks.yaml` + bt + ` (optional – the AKS API owns the cluster shape)

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
- ` + bt + `Talos` + bt + ` (Docker/Hetzner) → ` + bt + `admin@talos-default` + bt + `
- ` + bt + `Talos` + bt + ` (Omni) → the context name generated by Omni (e.g., ` + bt + `devantler-prod` + bt + `)
- ` + bt + `VCluster` + bt + ` → ` + bt + `vcluster-docker_vcluster-default` + bt + `
- ` + bt + `KWOK` + bt + ` → ` + bt + `kwok-kwok-default` + bt + `

When using Talos with Omni, Omni generates the context name; set ` + bt + `spec.cluster.connection.context` + bt + ` to that generated name.

**Timeout format:** Go duration string (e.g., ` + bt + `30s` + bt + `, ` + bt + `5m` + bt + `, ` + bt + `1h` + bt + `)`

// cniDetails provides prose after the CNI enum list.
const cniDetails = `See [CNI](/concepts/#container-network-interface-cni) for more details.

- ` + bt + `Default` + bt + ` (default) – Uses the distribution's built-in CNI (` + bt + `kindnetd` + bt + ` for Vanilla, ` + bt + `flannel` + bt + ` for K3s)
- ` + bt + `Cilium` + bt + ` – Installs [Cilium](https://cilium.io/) for advanced networking and observability
- ` + bt + `Calico` + bt + ` – Installs [Calico](https://www.tigera.io/project-calico/) for network policies`

// csiDetails provides prose after the CSI enum list.
const csiDetails = `See [CSI](/concepts/#container-storage-interface-csi) for more details.

- ` + bt + `Default` + bt + ` (default) – Uses the distribution × provider's default behavior:
  - K3s: includes local-path-provisioner
  - Vanilla/Talos × Docker: no CSI
  - Talos × Hetzner: includes Hetzner CSI driver
- ` + bt + `Enabled` + bt + ` – Explicitly installs CSI driver (local-path-provisioner for local clusters, Hetzner CSI for Talos × Hetzner)
- ` + bt + `Disabled` + bt + ` – Disables CSI installation (for K3s, this disables the default local-storage)`

// metricsServerDetails provides prose after the MetricsServer enum list.
const metricsServerDetails = `Whether to install [metrics-server](/concepts/#metrics-server) for resource metrics.

- ` + bt + `Default` + bt + ` (default) – Uses distribution's default behavior (K3s includes metrics-server; Vanilla and Talos do not)
- ` + bt + `Enabled` + bt + ` – Install metrics-server
- ` + bt + `Disabled` + bt + ` – Skip installation

When metrics-server is enabled on Vanilla or Talos, KSail automatically:

1. Configures kubelet certificate rotation (` + bt + `serverTLSBootstrap: true` + bt + `)
2. Installs [kubelet-csr-approver](/concepts/#kubelet-csr-approver) to approve certificate requests
3. Deploys metrics-server with secure TLS communication`

// certManagerDetails provides prose after the CertManager enum list.
const certManagerDetails = `Whether to install [cert-manager](/concepts/#cert-manager) for TLS certificate management.

- ` + bt + `Enabled` + bt + ` – Install cert-manager
- ` + bt + `Disabled` + bt + ` (default) – Skip installation`

// policyEngineDetails provides prose after the PolicyEngine enum list.
const policyEngineDetails = `Policy engine to install for enforcing security, compliance, and best practices. See [Policy Engines](/concepts/#policy-engines) for details.

- ` + bt + `None` + bt + ` (default) – No policy engine
- ` + bt + `Kyverno` + bt + ` – Install [Kyverno](https://kyverno.io/)
- ` + bt + `Gatekeeper` + bt + ` – Install [OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/)`

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
const gitOpsEngineDetails = `GitOps engine for continuous deployment. See [GitOps](/concepts/#gitops). When set to ` + bt + `Flux` + bt + ` or ` + bt + `ArgoCD` + bt + `, KSail scaffolds a GitOps CR into your source directory.

- ` + bt + `None` + bt + ` (default) – No GitOps engine
- ` + bt + `Flux` + bt + ` – Install [Flux CD](https://fluxcd.io/) and scaffold FluxInstance CR
- ` + bt + `ArgoCD` + bt + ` – Install [Argo CD](https://argo-cd.readthedocs.io/) and scaffold Application CR`

// configDistToolOptions describes distribution and tool-specific options.
const configDistToolOptions = `#### Distribution and Tool Options

Advanced configuration options are direct fields under ` + bt + `spec.cluster` + bt + `. See [Schema Support](#schema-support) for the complete structure.

**Talos options (` + bt + `spec.cluster.talos` + bt + `):**

- ` + bt + `controlPlanes` + bt + ` – Number of control-plane nodes (default: ` + bt + `1` + bt + `)
- ` + bt + `workers` + bt + ` – Number of worker nodes (default: ` + bt + `0` + bt + `)
- ` + bt + `config` + bt + ` – Path to talosconfig file (default: ` + bt + `~/.talos/config` + bt + `)
- ` + bt + `version` + bt + ` – Pin the Talos OS version (e.g. ` + bt + `v1.12.4` + bt + `); caps upgrades and selects the node image (default: built-in)
- ` + bt + `iso` + bt + ` – Cloud provider ISO/image ID for Talos Linux (default: ` + bt + `125127` + bt + ` for Talos 1.12.4 x86; for ARM, look up the matching ISO ID under **Images → ISOs** in the Hetzner Cloud Console)

The Kubernetes version is set at the top level (` + bt + `spec.cluster.kubernetesVersion` + bt + `), not under ` + bt + `talos` + bt + `:

- ` + bt + `kubernetesVersion` + bt + ` – Pin the Kubernetes version (e.g. ` + bt + `v1.32.0` + bt + `). When unset, ` + bt + `cluster update` + bt + ` keeps the version already running (no unrequested upgrade) and new clusters default to one compatible with the pinned ` + bt + `talos.version` + bt + `

**Provider options (` + bt + `spec.provider` + bt + `):** infrastructure provider options (Hetzner, Omni, AWS, GCP, Kubernetes) are documented in the generated [spec.provider (ProviderSpec)](#specprovider-providerspec) sections below.

**Autoscaler options (` + bt + `spec.cluster.autoscaler` + bt + `):** pod and node autoscaling options are documented in the generated [spec.cluster.autoscaler (AutoscalerConfig)](#specclusterautoscaler-autoscalerconfig) sections below.

**Vanilla options (` + bt + `spec.cluster.vanilla` + bt + `):**

- ` + bt + `mirrorsDir` + bt + ` – Directory for containerd host mirror configuration`

// configDistributionConfigProse describes distribution configuration files.
const configDistributionConfigProse = `## Distribution Configuration

KSail references distribution-specific configuration files to customize cluster behavior. The path to these files is set via ` + bt + `spec.cluster.distributionConfig` + bt + `.

### Vanilla (implemented with Kind) Configuration

**Default:** ` + bt + `kind.yaml` + bt + `

See [Kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/) for the full schema.

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

See [K3d Configuration](https://k3d.io/stable/usage/configfile/) for the full schema.

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

Talos uses a directory structure for [Talos machine configuration patches](https://www.talos.dev/latest/reference/configuration/). Place YAML patch files in ` + bt + `talos/cluster/` + bt + ` (all nodes), ` + bt + `talos/control-planes/` + bt + `, or ` + bt + `talos/workers/` + bt + `:

` + cbt + `yaml
# talos/cluster/kubelet.yaml  (applies to all nodes)
machine:
  kubelet:
    extraArgs:
      max-pods: "250"
` + cbt + `

See [Talos Configuration Reference](https://www.talos.dev/latest/reference/configuration/) for patch syntax. Use ` + bt + `spec.cluster.talos` + bt + ` to configure node counts:

` + cbt + `yaml
spec:
  cluster:
    distribution: Talos
    distributionConfig: talos
    talos:
      controlPlanes: 3
      workers: 2
` + cbt + `

#### Port Mappings (Docker Provider)

On macOS, Docker runs in a Linux VM, so MetalLB virtual IPs are not accessible from the host. Use ` + bt + `extraPortMappings` + bt + ` to expose container ports directly:

` + cbt + `yaml
spec:
  cluster:
    distribution: Talos
    talos:
      extraPortMappings:
        - containerPort: 80
          hostPort: 8080
          protocol: TCP
        - containerPort: 443
          hostPort: 8443
          protocol: TCP
` + cbt + `

Access services at ` + bt + `http://localhost:8080` + bt + `. Ports are exposed on the first control-plane node; in multi-control-plane clusters, ` + bt + `extraPortMappings` + bt + ` apply only to that node.`

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

IDEs with YAML language support (e.g., VS Code + [Red Hat YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)) provide field autocompletion, inline docs, validation, and enum suggestions.`
