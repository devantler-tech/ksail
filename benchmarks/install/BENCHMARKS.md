# Component Install Duration Benchmarks

This document describes the component install duration tracking in KSail and provides guidance for collecting baseline performance metrics.

## Overview

KSail tracks per-component install durations during `ksail cluster create` and `ksail cluster update`. Each component install step (CNI, CSI, metrics-server, load-balancer, cert-manager, policy engine, GitOps engines) records its elapsed time. Durations are surfaced via the `--benchmark` flag.

## Viewing Install Durations

```bash
# Create a cluster with per-component timing output
ksail cluster create --benchmark

# Update a cluster with per-component timing output
ksail cluster update --benchmark
```

Example output (CI mode):

```
📦 Installing infrastructure components...
► metrics-server installing
► csi installing
✔ metrics-server installed [2.1s]
✔ csi installed [3.4s]
⏲ current: 3.4s
  total:  12.8s

📦 Installing GitOps engines...
► flux installing
✔ flux installed [5.2s]
⏲ current: 5.2s
  total:  18.0s
```

## Collecting Baseline Measurements

To establish baseline install durations for a specific distribution and component combination:

```bash
# Create a cluster with benchmark output and capture timing
ksail cluster create --benchmark 2>&1 | tee install-baseline.log

# Extract per-component durations from the log
grep -E '✔.*installed \[' install-baseline.log
```

## Distribution × Component Matrix

Install durations vary by distribution and provider. The following matrix documents which components KSail installs as separate Helm charts when explicitly enabled in `ksail.yaml`. Components marked `—` are either bundled by the distribution (e.g., K3s bundles metrics-server and local-path CSI), managed by the host cluster (VCluster delegates CNI/networking to the host), or not applicable for that distribution.

> **Note:** This table reflects explicit `ksail.yaml` configuration with all optional components enabled. Default configurations may install fewer components depending on the distribution's built-in capabilities.

| Component      | Vanilla (Kind) | K3s (K3d) | Talos (Docker) | VCluster (Vind) |
|----------------|:--------------:|:---------:|:--------------:|:---------------:|
| CNI (Cilium)   |       ✔        |     ✔     |       ✔        |        —        |
| CNI (Calico)   |       ✔        |     ✔     |       ✔        |        —        |
| CSI            |       ✔        |     —     |       ✔        |        —        |
| metrics-server |       ✔        |     —     |       ✔        |        ✔        |
| load-balancer  |       —        |     —     |       ✔        |        —        |
| cert-manager   |       ✔        |     ✔     |       ✔        |        ✔        |
| policy-engine  |       ✔        |     ✔     |       ✔        |        ✔        |
| Flux           |       ✔        |     ✔     |       ✔        |        ✔        |
| ArgoCD         |       ✔        |     ✔     |       ✔        |        ✔        |

- **K3s (K3d)**: Bundles metrics-server and local-path-provisioner CSI by default; KSail does not install these separately.
- **VCluster (Vind)**: CNI and networking are managed by the host cluster; CSI is managed by the vCluster chart. KSail installs metrics-server with `--kubelet-insecure-tls` for VCluster compatibility.

## Benchmark Format

Per-component durations are displayed as `[duration]` suffixes on completion lines (e.g., `✔ metrics-server installed [2.1s]`). Group-level timing shows stage and total elapsed:

```
⏲ current: {stage_duration}
  total:  {total_duration}
```

This format is consistent with the existing timing output from `pkg/timer/` and `pkg/notify/` packages.

## Performance Notes

- Component install durations are dominated by Helm chart downloads, image pulls, and pod scheduling — they are not suitable for micro-benchmarking with `go test -bench`.
- Use the `--benchmark` flag to capture real-world install timings in CI or local environments.
- Compare durations across runs to identify regressions (e.g., timeout increases, slow image pulls).
- The diff engine benchmarks (see `pkg/svc/diff/BENCHMARKS.md`) and display benchmarks (see `pkg/cli/cmd/cluster/BENCHMARKS.md`) cover the computational aspects; this document covers the I/O-bound install layer.
