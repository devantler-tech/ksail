# ConfigManager Benchmarks

This document describes the benchmark suite for KSail's config manager and provides baseline performance metrics.

## Overview

The config manager benchmarks measure the performance of the configuration load hot path executed on every KSail command invocation. This includes Viper initialisation, parent-directory traversal (to locate `ksail.yaml`), YAML decoding via mapstructure, environment-variable expansion, and field-selector default application.

## Running Benchmarks

```bash
# Run all config manager benchmarks
go test -run=^$ -bench=. -benchmem ./pkg/fsutil/configmanager/ksail/...

# Run a specific benchmark
go test -run=^$ -bench='^BenchmarkLoad_WithConfigFile$' -benchmem ./pkg/fsutil/configmanager/ksail/...

# Save results for comparison
go test -run=^$ -bench=. -benchmem ./pkg/fsutil/configmanager/ksail/... > baseline.txt

# Compare before/after changes
go test -run=^$ -bench=. -benchmem ./pkg/fsutil/configmanager/ksail/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

| Benchmark                                 | Description                                                                                                                                                                                                   |
|-------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `BenchmarkInitializeViper`                | `NewConfigManager` with no field selectors: Viper init, file-type/config-name settings, parent-directory traversal, and env-var binding                                                                       |
| `BenchmarkNewConfigManager_WithSelectors` | Same as above but with the four typical production field selectors registered (distribution, provider, source directory, distribution config)                                                                 |
| `BenchmarkLoad_NoConfigFile`              | Full `Load()` cycle when no `ksail.yaml` exists in the working directory tree (e.g., `ksail cluster init`) — includes Viper ReadInConfig miss, mapstructure Unmarshal, and field-selector default application |
| `BenchmarkLoad_WithConfigFile`            | Full `Load()` cycle when a valid `ksail.yaml` is present — the operational hot path for all cluster lifecycle commands                                                                                        |
| `BenchmarkLoad_WithConfigFile_DeepTree`   | Same as `BenchmarkLoad_WithConfigFile` but run from a 10-level deep subdirectory; measures the extra `os.Stat()` overhead of the parent-directory traversal                                                   |
| `BenchmarkLoad_Cached`                    | Second `Load()` call on the same manager after the cache has been warmed; should return in nanoseconds with zero allocations                                                                                  |

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent. Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks).

Example baseline (AMD EPYC 7763, Linux, Go 1.26.0, captured in [#3134](https://github.com/devantler-tech/ksail/pull/3134)):

```text
BenchmarkInitializeViper-4                    18335 ns/op    6416 B/op    77 allocs/op
BenchmarkNewConfigManager_WithSelectors-4     16858 ns/op    6416 B/op    77 allocs/op
BenchmarkLoad_NoConfigFile-4                  62419 ns/op   21489 B/op   463 allocs/op
BenchmarkLoad_WithConfigFile-4               162055 ns/op   67762 B/op  1030 allocs/op
BenchmarkLoad_WithConfigFile_DeepTree-4      149671 ns/op   62251 B/op   956 allocs/op
BenchmarkLoad_Cached-4                           2.9 ns/op      0 B/op     0 allocs/op
```

## Interpreting Results

| Column      | Description                                                |
|-------------|------------------------------------------------------------|
| `N`         | Number of iterations                                       |
| `ns/op`     | Nanoseconds per operation (lower is better)                |
| `B/op`      | Bytes allocated per operation (lower is better)            |
| `allocs/op` | Number of heap allocations per operation (lower is better) |

## Optimization Opportunities

- **Decode hook**: The `clusterDecodeHook` (`ComposeDecodeHookFunc` + `metav1DurationDecodeHook`) is precomputed once as a package-level variable in `decode_hooks.go`, avoiding 3 heap allocations per `Load()` call.
- **Viper traversal**: `InitializeViper` walks every ancestor directory up to the filesystem root issuing an `os.Stat` per level. Caching the discovered config root across invocations (e.g., via an environment variable set by the CLI wrapper) could reduce this overhead for deeply nested working directories.
- **mapstructure allocations**: `BenchmarkLoad_WithConfigFile` accounts for ~1030 allocs/op. The majority originate inside mapstructure's reflection-based decoder, which operates on an intermediate `map[string]any` produced by Viper. A future optimisation could unmarshal the YAML file directly into the target struct using `gopkg.in/yaml.v3` (or `sigs.k8s.io/yaml`), bypassing the intermediate map and the mapstructure step entirely and significantly reducing allocation overhead.
- **Cache hit path**: `BenchmarkLoad_Cached` shows the cache is effectively free (2.9 ns / 0 allocs). Ensure callers that invoke `Load()` more than once per command reuse the same `ConfigManager` instance.
