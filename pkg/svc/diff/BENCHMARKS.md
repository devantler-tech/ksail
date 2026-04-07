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

- **ComputeDiff_NoChanges**: Identical old and new specs — the common case when re-running `ksail cluster update` after the cluster already matches config. (~367 ns/op)

### In-Place Changes

- **ComputeDiff_AllInPlaceChanges**: All seven component fields (CNI, CSI, metrics-server, load-balancer, cert-manager, policy engine, GitOps engine) changed; all categorized as in-place. (~794 ns/op)

### Recreate-Required Changes

- **ComputeDiff_RecreateRequired**: Distribution change from Vanilla to K3s, which requires cluster recreation. (~399 ns/op)

### Mixed Categories

- **ComputeDiff_MixedCategories**: Realistic mix of recreate-required (distribution + registry) and in-place (CNI + cert-manager) changes. (~572 ns/op)

### Distribution-Specific

- **ComputeDiff_TalosOptions**: Talos-specific field changes (control planes, workers, ISO) with the Talos distribution. (~665 ns/op)
- **ComputeDiff_HetznerOptions**: Hetzner-specific field changes (server type, SSH key) with the Hetzner provider. (~622 ns/op)

### Edge Cases

- **ComputeDiff_NilSpec**: Fast nil-spec path — taken when either the old or new spec is `nil`; this path skips all comparisons. This is a defensive guard path; current provisioners return non-nil specs with defaults or detected values. (~51 ns/op)

## Baseline Results

Approximate baseline on a typical CI runner — `ns/op` (time/op) varies by hardware and load, while `B/op` and `allocs/op` primarily vary with Go version, architecture, and code changes:

| Benchmark                     | ns/op | B/op | allocs/op |
|-------------------------------|------:|-----:|----------:|
| ComputeDiff_NoChanges         |   367 |  832 |         5 |
| ComputeDiff_AllInPlaceChanges |   794 | 1984 |         9 |
| ComputeDiff_RecreateRequired  |   399 |  912 |         6 |
| ComputeDiff_MixedCategories   |   572 | 1280 |         9 |
| ComputeDiff_TalosOptions      |   665 | 1360 |        10 |
| ComputeDiff_HetznerOptions    |   622 | 1072 |         9 |
| ComputeDiff_NilSpec           |    51 |  144 |         1 |

## Optimization History

### 2026-04-07: Eliminate per-call allocations for static field-rule tables

`talosFieldRules` and `hetznerFieldRules` were previously plain functions that
returned a freshly allocated `[]fieldRule` slice on every `ComputeDiff` call.
Converting them to package-level variables eliminates these allocations entirely.

The `allocs/op` counts above have been updated to reflect this change.

## Performance Notes

- The nil-spec fast path is ~7× faster than the no-changes path — it is taken only when either the old or new spec is `nil` (e.g., defensive guard paths); current provisioners typically pass non-nil specs with defaults/detected values.
- `Change` values are appended to a slice by value; allocations primarily come from slice growth and any string formatting/conversions rather than from per-change struct allocations.
- The `strings.Builder` pre-allocation in the display pipeline (see `pkg/cli/cmd/cluster/BENCHMARKS.md`) is a separate concern from `ComputeDiff` itself.
