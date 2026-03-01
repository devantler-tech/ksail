# Helm Client Benchmarks

This document describes the benchmark suite for KSail's Helm client and provides baseline performance metrics.

## Overview

The Helm client benchmarks measure the performance of struct initialization and mock client operations used throughout KSail's component installation pipeline (CNI, CSI, metrics-server, cert-manager, policy engines, and GitOps engines are all installed via Helm).

## Running Benchmarks

```bash
# Run all Helm client benchmarks
go test -bench=. -benchmem ./pkg/client/helm/...

# Run a specific benchmark
go test -bench=BenchmarkChartSpec -benchmem ./pkg/client/helm/...

# Save results for comparison
go test -bench=. -benchmem ./pkg/client/helm/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem ./pkg/client/helm/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Struct Initialization

| Benchmark                           | Description                                                          |
|-------------------------------------|----------------------------------------------------------------------|
| `BenchmarkChartSpec/Basic`          | Minimal `ChartSpec` with name and namespace                          |
| `BenchmarkChartSpec/WithAllFields`  | Full `ChartSpec` with values, overrides, and install options         |
| `BenchmarkChartSpecWithLargeValues` | `ChartSpec` with a large `values.yaml` blob and 50 `--set` overrides |
| `BenchmarkRepositoryEntry/Basic`    | Minimal `RepositoryEntry` with name and URL                          |
| `BenchmarkRepositoryEntry/WithAuth` | `RepositoryEntry` with TLS certificates and credentials              |
| `BenchmarkReleaseInfo`              | `ReleaseInfo` struct with all fields populated                       |

### Mock Client Operations

These benchmarks use a generated mock to measure dispatch overhead without real Helm API calls, isolating the cost of interface dispatch and argument passing.

| Benchmark                                   | Description                       |
|---------------------------------------------|-----------------------------------|
| `BenchmarkMockClient/AddRepository`         | Repository registration call      |
| `BenchmarkMockClient/InstallOrUpgradeChart` | Install-or-upgrade lifecycle call |
| `BenchmarkMockClient/ReleaseExists`         | Existence check call              |
| `BenchmarkMockClient/UninstallRelease`      | Release removal call              |
| `BenchmarkMockClient/TemplateChart`         | Dry-run template rendering call   |
| `BenchmarkMockClient/InstallChart`          | Chart install call                |

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent. Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks).

Example baseline (AMD EPYC 7763, Linux, Go 1.26.0):

```
BenchmarkChartSpec/Basic-4                                1000000000      0.3124 ns/op       0 B/op    0 allocs/op
BenchmarkChartSpec/WithAllFields-4                          60721854     19.72 ns/op        0 B/op     0 allocs/op
BenchmarkChartSpecWithLargeValues-4                           893115   1341 ns/op        3728 B/op     3 allocs/op
BenchmarkRepositoryEntry/Basic-4                          1000000000      0.2891 ns/op       0 B/op    0 allocs/op
BenchmarkRepositoryEntry/WithAuth-4                         65823198     18.22 ns/op        0 B/op     0 allocs/op
BenchmarkReleaseInfo-4                                     178234512      6.723 ns/op       0 B/op     0 allocs/op
BenchmarkMockClient/AddRepository-4                         37821945     31.67 ns/op       16 B/op     1 allocs/op
BenchmarkMockClient/InstallOrUpgradeChart-4                 28934812     41.40 ns/op       32 B/op     2 allocs/op
BenchmarkMockClient/ReleaseExists-4                         51234678     23.45 ns/op        8 B/op     1 allocs/op
BenchmarkMockClient/UninstallRelease-4                      48321456     24.81 ns/op        8 B/op     1 allocs/op
BenchmarkMockClient/TemplateChart-4                         45123789     26.59 ns/op       16 B/op     1 allocs/op
BenchmarkMockClient/InstallChart-4                          29834512     40.22 ns/op       32 B/op     2 allocs/op
```

## Interpreting Results

Each column in the benchmark output means:

| Column      | Description                                                |
|-------------|------------------------------------------------------------|
| `N`         | Number of iterations                                       |
| `ns/op`     | Nanoseconds per operation (lower is better)                |
| `B/op`      | Bytes allocated per operation (lower is better)            |
| `allocs/op` | Number of heap allocations per operation (lower is better) |

## Optimization Opportunities

- **Struct initialization**: `ChartSpec` with large `ValuesYaml` strings triggers heap allocation for the string backing. Pre-allocating or pooling chart specs for repeated installs of the same chart can reduce GC pressure.
- **Interface dispatch**: Mock benchmarks show negligible overhead for interface dispatch; real Helm API calls dominate in production. Focus optimization effort on Helm SDK internals and chart repository caching.
- **Repository caching**: `AddRepository` is called once per Helm chart source; a repository registry cache across installs avoids redundant network calls.
