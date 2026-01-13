# KSail System Test Action

A GitHub composite action that runs a full end-to-end system test of KSail, testing cluster lifecycle and workload management capabilities.

## What It Tests

1. **Cluster Init** (optional) - Initialize a new KSail project
2. **Cluster Create** - Create and start a Kubernetes cluster
3. **Cluster List** - Verify cluster appears in list
4. **Workload Create** - Create a deployment imperatively
5. **Workload Apply** (optional) - Apply a kustomize overlay
6. **Workload Push & Reconcile** (GitOps only) - Test GitOps workflow
7. **Cluster Stop** - Stop the running cluster
8. **Cluster Start** - Start the stopped cluster
9. **Cluster Delete** - Clean up the cluster

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
  with:
    distribution: Talos
    provider: Hetzner
    args: ""
    hcloud-token: ${{ secrets.HCLOUD_TOKEN }}
```

> **Note:** Only Talos distribution supports Hetzner provider.

## Inputs

| Input                 | Description                                             | Required | Default                 |
| --------------------- | ------------------------------------------------------- | -------- | ----------------------- |
| `distribution`        | Kubernetes distribution (Vanilla, K3s, Talos)           | Yes      | -                       |
| `provider`            | Infrastructure provider (Docker, Hetzner)               | No       | `Docker`                |
| `args`                | Additional arguments for cluster init/create            | No       | `""`                    |
| `init`                | Run `ksail cluster init` before create                  | No       | `false`                 |
| `test-workload-image` | Image for workload create test                          | No       | `traefik/whoami:latest` |
| `test-workload-name`  | Name for test workload deployment                       | No       | `whoami`                |
| `apply-overlay-path`  | Path to kustomize overlay for apply test                | No       | `""`                    |
| `gitops-path`         | Path for GitOps push                                    | No       | `k8s`                   |
| `workload-timeout`    | Timeout for workload wait operations                    | No       | `300s`                  |
| `hcloud-token`        | Hetzner Cloud API token (required for Hetzner provider) | No       | `""`                    |

## Prerequisites

- **KSail binary** must be installed and in PATH
- **Docker** must be running (for Docker provider)
- **HCLOUD_TOKEN** must be set (for Hetzner provider)
- Sufficient disk space (use `free-disk-space` action for CI runners)

## Failure Debugging

When workload steps fail, the action automatically runs the `debug-kubernetes-failure` action to output:

- Disk usage
- Node status and conditions
- Pod status
- Recent cluster events

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
