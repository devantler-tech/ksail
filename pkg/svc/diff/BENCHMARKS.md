# Diff Engine Benchmarks

This document describes the benchmark suite for KSail's diff engine and provides baseline performance metrics.

## Overview

The diff engine benchmarks measure performance of `ComputeDiff`, which compares old and new `ClusterSpec` values and classifies each change into one of three categories: in-place, reboot-required, or recreate-required. This is called on every `ksail cluster update` invocation.

## Running Benchmarks

```bash
# Run all diff engine benchmarks
go test -bench=. -benchmem ./pkg/svc/diff/...

# Run a specific benchmark
go test -bench=BenchmarkComputeDiff_NoChanges -benchmem ./pkg/svc/diff/...

# Save results for comparison
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/svc/diff/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/svc/diff/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Steady-State (No Changes)

- **ComputeDiff_NoChanges**: Identical old and new specs — the common case when re-running `ksail cluster update` after the cluster already matches config. (~987 ns/op)

### In-Place Changes

- **ComputeDiff_AllInPlaceChanges**: All seven component fields (CNI, CSI, metrics-server, load-balancer, cert-manager, policy engine, GitOps engine) changed; all categorized as in-place. (~3808 ns/op)

### Recreate-Required Changes

- **ComputeDiff_RecreateRequired**: Distribution change from Vanilla to K3s, which requires cluster recreation. (~1596 ns/op)

### Mixed Categories

- **ComputeDiff_MixedCategories**: Realistic mix of recreate-required (distribution + registry) and in-place (CNI + cert-manager) changes. (~2509 ns/op)

### Distribution-Specific

- **ComputeDiff_TalosOptions**: Talos-specific field changes (control planes, workers, ISO) with the Talos distribution. (~2528 ns/op)
- **ComputeDiff_HetznerOptions**: Hetzner-specific field changes (server type, SSH key) with the Hetzner provider. (~2547 ns/op)

### Edge Cases

- **ComputeDiff_NilSpec**: Fast nil-spec path — taken when either the old or new spec is `nil`; this path skips all comparisons. This is a defensive guard path; current provisioners return non-nil specs with defaults or detected values. (~202 ns/op)

## Baseline Results

Approximate baseline on a typical CI runner — `ns/op` (time/op) varies by hardware and load, while `B/op` and `allocs/op` primarily vary with Go version, architecture, and code changes:

| Benchmark                     | ns/op | B/op | allocs/op |
|-------------------------------|------:|-----:|----------:|
| ComputeDiff_NoChanges         |   987 |  480 |         8 |
| ComputeDiff_AllInPlaceChanges |  3808 | 2080 |        30 |
| ComputeDiff_RecreateRequired  |  1596 |  752 |        12 |
| ComputeDiff_MixedCategories   |  2509 | 1312 |        20 |
| ComputeDiff_TalosOptions      |  2528 | 1312 |        20 |
| ComputeDiff_HetznerOptions    |  2547 | 1312 |        20 |
| ComputeDiff_NilSpec           |   202 |   48 |         2 |

## Performance Notes

- The nil-spec fast path is ~5× faster than the no-changes path — it is taken only when either the old or new spec is `nil` (e.g., defensive guard paths); current provisioners typically pass non-nil specs with defaults/detected values.
- `Change` values are appended to a slice by value; allocations primarily come from slice growth and any string formatting/conversions rather than from per-change struct allocations.
- The `strings.Builder` pre-allocation in the display pipeline (see `pkg/cli/cmd/cluster/BENCHMARKS.md`) is a separate concern from `ComputeDiff` itself.
