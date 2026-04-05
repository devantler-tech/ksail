# KSail System Test Action

A GitHub composite action that orchestrates a full end-to-end system test of KSail. It delegates to focused sub-actions for modularity and maintainability.

## What It Tests

### Phase 1 — Setup

- Provider credential validation (Hetzner/Omni)
- Artifact tag generation for unique CI run identification
- GHCR credential resolution for external registry tests

### Phase 2 — Offline Tests (no cluster needed)

- **Gen & Smoke** (`ksail-test-gen-smoke`) — Generates and validates manifests for 15 resource types; smoke-tests `chat --help` and `mcp --help`
- **Tenant** (`ksail-tenant-test`) — Tests tenant create/delete for kubectl, flux (OCI + Git), and argocd types; covers multi-namespace, custom ClusterRole, force overwrite, and register/unregister

### Phase 3 — Cluster Provisioning

- **Cluster Init** (optional) — Initialize a new KSail project
- **Manifest Validate** (when `init` is enabled) — Validate generated manifests before cluster creation
- **Cluster Create** — Create and start a Kubernetes cluster
- **Cluster Info / List** — Verify cluster status and listing

### Phase 4 — Online Tests (cluster running)

- **Workload Lifecycle** (`ksail-test-workload`) — Full CRUD: create, get, describe, logs, scale, install (Helm), apply (kustomize), watch, push/reconcile (GitOps), delete
- **API-dependent Gen** — `workload gen clusterrole` and `role` (require API server for resource group discovery)
- **Backup & Restore** (`ksail-test-backup-restore`) — Deploy workload, backup cluster, delete workload, restore from backup, verify restoration
- **Cluster Update** — Dry-run and actual update with regression detection

### Phase 5 — Debug

- Automatic Kubernetes diagnostic output on failure

### Phase 6 — Lifecycle Tests

- **Cluster Stop** — Stop the running cluster
- **Cluster Start** — Start the stopped cluster (with retry)
- **Cluster Switch** — Switch kubeconfig context

### Phase 7 — Cleanup

- **Cluster Delete** — Clean up the cluster (always runs)

## Sub-Action Architecture

```
ksail-system-test (orchestrator)
├── ksail-test-gen-smoke        # Offline: gen + validate + smoke
├── ksail-tenant-test           # Offline: tenant create/delete
├── ksail-cluster               # Provisioning: init + create
├── ksail-test-workload         # Online: workload CRUD lifecycle
├── ksail-test-backup-restore   # Online: backup/restore cycle
└── debug-kubernetes-failure    # Debug: diagnostic output on failure
```

Adding a new test phase: create a new sub-action and add one `uses:` line to the orchestrator.

## Usage

### Basic Usage

```yaml
steps:
  - uses: actions/checkout@v4

  - name: Setup Go
    uses: actions/setup-go@v5
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
      - uses: actions/checkout@v4
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

> **Note:** Only Talos distribution supports Hetzner and Omni providers.

## Inputs

| Input                 | Description                                             | Required | Default                |
|-----------------------|---------------------------------------------------------|----------|------------------------|
| `distribution`        | Kubernetes distribution (Vanilla, K3s, Talos, VCluster) | Yes      | -                      |
| `provider`            | Infrastructure provider (Docker, Hetzner, Omni)         | No       | `Docker`               |
| `args`                | Additional arguments for cluster init/create            | No       | `""`                   |
| `init`                | Run `ksail cluster init` before create                  | No       | `false`                |
| `test-workload-image` | Image for workload create test                          | No       | `traefik/whoami:v1.10` |
| `test-workload-name`  | Name for test workload deployment                       | No       | `whoami`               |
| `apply-overlay-path`  | Path to kustomize overlay for apply test                | No       | `""`                   |
| `gitops-path`         | Path for GitOps push                                    | No       | `k8s`                  |
| `workload-timeout`    | Timeout for workload wait operations                    | No       | `600s`                 |
| `ghcr-user`           | GitHub Container Registry username                      | No       | `""`                   |
| `ghcr-token`          | GitHub Container Registry token                         | No       | `""`                   |

## Prerequisites

- **KSail binary** must be installed and in PATH
- **Docker** must be running (for Docker provider)
- **HCLOUD_TOKEN** env var must be set (for Hetzner provider)
- **OMNI_SERVICE_ACCOUNT_KEY** and **OMNI_ENDPOINT** env vars must be set (for Omni provider)
- Sufficient disk space (use `free-disk-space` action for CI runners)

## Failure Debugging

When tests fail, debug output is provided at two levels:

- **Sub-action debug**: Each sub-action (`ksail-test-workload`, `ksail-test-backup-restore`) includes its own `debug-kubernetes-failure` step scoped to its step outcomes
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
            init: false
            args: "--cni Cilium"
    steps:
      - uses: actions/checkout@v4

      - name: Free disk space
        uses: ./.github/actions/free-disk-space

      - name: Setup Go
        uses: actions/setup-go@v5
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
