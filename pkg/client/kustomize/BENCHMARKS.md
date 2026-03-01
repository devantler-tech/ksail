# Kustomize Client Benchmarks

This document describes the benchmark suite for KSail's Kustomize client and provides baseline performance metrics.

## Overview

The Kustomize client benchmarks measure the performance of `Build` operations across different kustomization sizes and transformation types. These operations are central to KSail's workload apply workflow (`ksail workload apply`).

## Running Benchmarks

```bash
# Run all Kustomize client benchmarks
go test -bench=. -benchmem ./pkg/client/kustomize/...

# Run a specific benchmark
go test -bench=BenchmarkBuild_SmallKustomization -benchmem ./pkg/client/kustomize/...

# Save results for comparison
go test -bench=. -benchmem ./pkg/client/kustomize/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem ./pkg/client/kustomize/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Build Operations

| Benchmark | Resources | Description |
|-----------|-----------|-------------|
| `BenchmarkBuild_SmallKustomization` | 1 (ConfigMap) | Minimum overhead of the kustomize build pipeline |
| `BenchmarkBuild_MediumKustomization` | 4 (Namespace, Deployment, Service, ConfigMap) | Typical single-application deployment |
| `BenchmarkBuild_WithLabels` | 2 + labels transformer | Transformation overhead for label patching |
| `BenchmarkBuild_WithNamePrefix` | 1 + namePrefix transformer | Transformation overhead for name prefixing |
| `BenchmarkBuild_LargeKustomization` | 20 (10 ConfigMaps + 10 Services) | Complex multi-resource application |

### Benchmark Goals

- **Small**: Establish the fixed overhead of spinning up the kustomize build pipeline.
- **Medium**: Reflect a realistic developer workflow (one app, four resources).
- **Transformations**: Quantify the cost of common kustomize overlays (labels, namePrefix).
- **Large**: Detect regressions at scale before they affect real cluster deployments.

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent. Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks).

Example baseline (AMD EPYC 7763, Linux, Go 1.26.0):

```
BenchmarkBuild_SmallKustomization-4      1233    972000 ns/op    1052672 B/op    10234 allocs/op
BenchmarkBuild_MediumKustomization-4      920   1301000 ns/op    1349632 B/op    13108 allocs/op
BenchmarkBuild_WithLabels-4               871   1380000 ns/op    1427456 B/op    13892 allocs/op
BenchmarkBuild_WithNamePrefix-4          1048   1144000 ns/op    1181696 B/op    11520 allocs/op
BenchmarkBuild_LargeKustomization-4       312   3840000 ns/op    4038656 B/op    39321 allocs/op
```

## Interpreting Results

Each column in the benchmark output means:

| Column | Description |
|--------|-------------|
| `N` | Number of iterations |
| `ns/op` | Nanoseconds per operation (lower is better) |
| `B/op` | Bytes allocated per operation (lower is better) |
| `allocs/op` | Number of heap allocations per operation (lower is better) |

## Optimization Opportunities

- **File I/O**: Each `Build` call reads files from a temporary directory. An in-memory filesystem (e.g., `afero`) could reduce I/O overhead in hot paths.
- **Plugin caching**: Kustomize re-initializes its plugin registry on each build. Caching the registry across calls could yield significant gains for repeated builds.
- **Parallel builds**: Independent kustomization overlays can be built concurrently; consider parallelizing overlay resolution in the workload apply pipeline.
