# KSail System Test Action

A GitHub composite action that orchestrates a full end-to-end system test of KSail. It delegates to focused sub-actions for modularity and maintainability.

## What It Tests

### Phase 1 — Setup

- Provider credential validation (Hetzner/Omni)
- Artifact tag generation for unique CI run identification
- GHCR credential resolution for external registry tests

There is no offline phase any more: manifest generation, the `open chat --help` / `open mcp --help` smoke checks and the tenant commands are covered by unit tests.

### Phase 3 — Cluster Provisioning

- **Project Init** (optional, `init: "true"`) — Initialize a new KSail project with `ksail project init`
- **Manifest Validate** (when `init` is enabled) — Validate generated manifests before cluster creation
- **Cluster Create** — Create and start a Kubernetes cluster
- **Cluster Info / List** — Verify cluster status and listing
- **Calico Migration** (optional) — Create with an immutable Calico 3.32.2 baseline binary, then use the candidate CLI to update without editing configuration. Verify prerequisite UIDs, the complete inventory, the deployed operator and node image, and an empty final diff. `calico-migration-binary` requires its producer's `calico-migration-sha256`; both artifacts are addressed by immutable IDs in CI.

### Phase 4 — Online Tests (cluster running)

- **Workload Lifecycle** (`ksail-test-workload`) — Full CRUD: create, get, describe, logs, scale, install (Helm), apply (kustomize), push/reconcile (GitOps), delete
- **Workload Mirror** (`ksail-test-workload-mirror`, optional) — Smoke-test `workload mirror` against a live HTTP deployment
- **Image Export & Import** (`ksail-test-image-export-import`)
- **Workload Watch** (`ksail-test-workload-watch`)
- **API-dependent Gen** — `workload gen clusterrole` and `role` (require API server for resource group discovery)
- **Backup & Restore** (`ksail-test-backup-restore`) — Deploy workload, backup cluster, delete workload, restore from backup, verify restoration
- **Cluster Update** — Dry-run and actual update with regression detection
- **Optional provider trials** — Kubernetes upgrade, Hetzner rolling resize, Talos schematic rollout, autoscaler inventory and floating IP toggle, each behind its own input (see [Inputs](#inputs))

### Phase 4b — Lifecycle Tests

- **Cluster Stop** — Stop the running cluster
- **Cluster Start** — Start the stopped cluster (with retry)

### Phase 4c — Kubernetes Provider Tests (optional)

- **Nested clusters** (`ksail-test-kubernetes-provider`, `test-kubernetes-provider: "true"`) — Create nested clusters on top of the running host cluster

### Phase 5 — Debug

- Automatic Kubernetes diagnostic output on failure, then **Cluster Switch** (switch kubeconfig context)
- Diagnostics and logs are uploaded as artifacts on failure (unless `upload-artifacts` is `"false"`)

### Phase 7 — Cleanup

- **Cluster Delete** (`ksail-system-test-cleanup`) — Deletes the cluster when `cleanup` is `"true"` (the default). A caller that must upload diagnostics first sets `cleanup: "false"` and runs the cleanup action itself afterwards; the Docker job in `ci.yaml` does this.

## Sub-Action Architecture

```text
ksail-system-test (orchestrator)
├── ksail-cluster                    # Provisioning: init + create
├── ksail-test-workload              # Online: workload CRUD lifecycle
├── ksail-test-workload-mirror       # Online: workload mirror smoke test (optional)
├── ksail-test-image-export-import   # Online: image export/import
├── ksail-test-workload-watch        # Online: workload watch
├── ksail-test-backup-restore        # Online: backup/restore cycle
├── ksail-test-kubernetes-provider   # Online: nested clusters (optional)
├── debug-kubernetes-failure         # Debug: diagnostic output on failure
└── ksail-system-test-cleanup        # Cleanup: cluster delete
```

Adding a new test phase: create a new sub-action and add one `uses:` line to the orchestrator.

## Usage

The action expects the `ksail` binary on `PATH`. In this repository the workflows get it from the `cache-ksail-binary` or `restore-ksail-binary` action; the examples below build it directly to stay self-contained.

### Basic Usage

```yaml
steps:
  - uses: actions/checkout@v7

  - name: Setup Go
    uses: actions/setup-go@v7
    with:
      go-version-file: go.mod

  - name: Build KSail
    run: go build -o ksail && sudo mv ksail /usr/local/bin/

  - name: Run system test
    uses: ./.github/actions/ksail-system-test
    with:
      distribution: Vanilla
```

### With Matrix Strategy

```yaml
jobs:
  system-test:
    strategy:
      fail-fast: false
      matrix:
        include:
          - distribution: Vanilla
            args: ""
          - distribution: K3s
            args: "--cni Cilium"
          - distribution: Talos
            args: "--gitops-engine Flux"
    steps:
      - uses: actions/checkout@v7
      - uses: ./.github/actions/ksail-system-test
        with:
          distribution: ${{ matrix.distribution }}
          args: ${{ matrix.args }}
```

### With Init and Apply

```yaml
- uses: ./.github/actions/ksail-system-test
  with:
    distribution: Vanilla
    init: "true"
    args: "--cni Cilium --gitops-engine Flux"
    apply-overlay-path: ".github/fixtures/podinfo-overlay"
```

### With Hetzner Provider

```yaml
- uses: ./.github/actions/ksail-system-test
  env:
    HCLOUD_TOKEN: ${{ secrets.HCLOUD_TOKEN }}
  with:
    distribution: Talos
    provider: Hetzner
    args: ""
```

> **Note:** Hetzner supports the Talos, K3s and Vanilla distributions. Omni supports Talos only.

### Manual same-version Talos schematic trial

Dispatch `system-test-hetzner.yaml` with `distribution=Talos`, `init=true` and
`test_schematic_rollout=true`. The optional `schematic_server_type` choice selects
`cx23`, `cx33` or `cpx22` for both node roles when the default class has no capacity.
Its `Default` selection preserves the generated server classes. Selection is explicit:
there is no automatic upsizing or retry across classes. The provider still checks
availability before creating resources, and the trial still requires image drift,
successful rollout, healthy nodes, converged readback and scoped cleanup.

The corresponding action input is `talos-schematic-server-type`; it is rejected
unless the initialized Hetzner/Talos schematic trial is enabled. The configuration
editor requires Mike Farah's `yq`, provided by the workflow runner image.

## Inputs

Cloud credentials are read from environment variables, not inputs (see [Prerequisites](#prerequisites)).

### Core

| Input                 | Description                                                   | Required | Default                |
|-----------------------|---------------------------------------------------------------|----------|------------------------|
| `distribution`        | Kubernetes distribution (Vanilla, K3s, Talos, VCluster, KWOK) | Yes      | -                      |
| `provider`            | Infrastructure provider (Docker, Hetzner, Omni)               | No       | `Docker`               |
| `args`                | Additional arguments for project init/create                  | No       | `""`                   |
| `init`                | Run `ksail project init` before create                        | No       | `false`                |
| `test-workload-image` | Image for workload create test                                | No       | `traefik/whoami:v1.10` |
| `test-workload-name`  | Name for test workload deployment                             | No       | `whoami`               |
| `apply-overlay-path`  | Path to kustomize overlay for apply test                      | No       | `""`                   |
| `gitops-path`         | Path for GitOps push (only with `--gitops-engine` in `args`)  | No       | `k8s`                  |
| `workload-timeout`    | Timeout for workload wait operations                          | No       | `600s`                 |

### Registries

| Input                 | Description                                                          | Default                      |
|-----------------------|----------------------------------------------------------------------|------------------------------|
| `ghcr-user`           | GitHub Container Registry username (external registry tests)         | `""`                         |
| `ghcr-token`          | GitHub Container Registry token (external registry tests)            | `""`                         |
| `dockerhub-user`      | Docker Hub username, so nested clusters pull through authenticated mirrors | `""`                   |
| `dockerhub-token`     | Docker Hub token for the same purpose                                | `""`                         |
| `ecr-public-upstream` | Upstream for the ECR Public mirror and containerd fallback           | `https://ecr-public.aws.com` |

### Optional trials

Each of these adds a phase and is off unless set. The action's `action.yaml` describes the exact preconditions of each.

| Input                               | Description                                                                              | Default                      |
|-------------------------------------|------------------------------------------------------------------------------------------|------------------------------|
| `test-workload-mirror`              | Smoke-test `workload mirror` against a live HTTP deployment                              | `false`                      |
| `test-kubernetes-provider`          | Create nested clusters on the running host cluster                                       | `false`                      |
| `kubernetes-provider-distributions` | Comma-separated distributions for the nested clusters                                    | `Vanilla,K3s,VCluster,Talos` |
| `kubernetes-provider-cni`           | CNI to install in each nested cluster (e.g. `Calico`)                                    | `""`                         |
| `assert-k3k-server-creator`         | Assert and record which principal created the nested K3s server pod                      | `false`                      |
| `calico-migration-binary`           | Verified baseline binary that creates a legacy Calico cluster before the candidate updates it | `""`                    |
| `calico-migration-sha256`           | SHA-256 from the producer of that baseline binary                                        | `""`                         |
| `kubernetes-upgrade-from`           | Starting Kubernetes version for the Docker/Talos upgrade trial                           | `""`                         |
| `kubernetes-upgrade-to`             | Target Kubernetes version for the Docker/Talos upgrade trial                             | `""`                         |
| `rolling-resize-server-type`        | Hetzner only: server type to resize to, exercising rolling node replacement              | `""`                         |
| `test-talos-schematic-rollout`      | Hetzner Talos only: change the Image Factory schematic and verify the rollout            | `false`                      |
| `talos-schematic-server-type`       | Hetzner class for that trial (`cx23`, `cx33` or `cpx22`); empty keeps the defaults       | `""`                         |
| `test-autoscaler-inventory`         | Talos/Hetzner orphaned-server audit with one powered-off probe                           | `false`                      |
| `inventory-trial-server-type`       | Server type shared by the cluster and the inventory probe                                | `cx23`                       |
| `inventory-trial-location`          | Location shared by the cluster and the inventory probe                                   | `fsn1`                       |
| `floating-ip-toggle`                | Hetzner Talos only: enable then disable the floating IP and verify the API endpoint each time | `false`                 |

### Diagnostics and cleanup

| Input                 | Description                                                                                         | Default |
|-----------------------|-----------------------------------------------------------------------------------------------------|---------|
| `upload-artifacts`    | Upload diagnostics and logs from inside the action                                                  | `true`  |
| `comprehensive-debug` | Run the full Kubernetes debug action after a failure; the bounded core diagnostics run either way   | `true`  |
| `cleanup`             | Delete the cluster inside the action                                                                | `true`  |

## Outputs

| Output         | Description                                                    |
|----------------|----------------------------------------------------------------|
| `artifact-tag` | Unique suffix for this matrix entry's diagnostic artifacts     |

## Prerequisites

- **KSail binary** must be installed and in PATH
- **Docker** must be running (for Docker provider)
- **HCLOUD_TOKEN** env var must be set (for Hetzner provider)
- **OMNI_SERVICE_ACCOUNT_KEY** and **OMNI_ENDPOINT** env vars must be set (for Omni provider)
- Sufficient disk space (use the `free-disk-space` action for CI runners)

## Failure Debugging

When tests fail, debug output is provided at two levels:

- **Sub-action debug**: The workload, backup/restore, image export/import and workload watch sub-actions each include their own `debug-kubernetes-failure` step scoped to their step outcomes
- **Orchestrator debug**: The orchestrator captures cluster-level and online-gen failures

## Example Workflow

```yaml
name: System Tests
on: [push, pull_request]

jobs:
  system-test:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          - distribution: Vanilla
            provider: Docker
            init: true
            args: ""
          - distribution: K3s
            provider: Docker
            init: true
            args: "--cni Cilium"
    steps:
      - uses: actions/checkout@v7

      - name: Free disk space
        uses: ./.github/actions/free-disk-space
        with:
          docker-prune: "true"

      - name: Setup Go
        uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Build and install KSail
        run: |
          go build -o ksail
          sudo mv ksail /usr/local/bin/

      - name: Run system test
        uses: ./.github/actions/ksail-system-test
        with:
          distribution: ${{ matrix.distribution }}
          provider: ${{ matrix.provider }}
          init: ${{ matrix.init }}
          args: ${{ matrix.args }}
          apply-overlay-path: ".github/fixtures/podinfo-overlay"
```
