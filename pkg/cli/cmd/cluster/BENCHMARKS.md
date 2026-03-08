# Cluster Update Display Benchmarks

This document describes the benchmark suite for the `ksail cluster update` diff table formatter and provides baseline performance metrics.

## Overview

The cluster update benchmarks measure performance of `formatDiffTable`, which renders the before/after diff table shown to users during `ksail cluster update`. The table has four columns (Component, Before, After, Impact) and rows ordered by severity (🔴 recreate → 🟡 reboot → 🟢 in-place).

## Running Benchmarks

```bash
# Run all cluster update benchmarks
go test -bench=. -benchmem ./pkg/cli/cmd/cluster/...

# Run a specific benchmark
go test -bench=BenchmarkFormatDiffTable_SingleChange -benchmem ./pkg/cli/cmd/cluster/...

# Save results for comparison
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/cli/cmd/cluster/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/cli/cmd/cluster/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### By Row Count

- **FormatDiffTable_SingleChange**: 1 in-place row — the smallest non-zero input. (~1359 ns/op, 16 allocs/op)
- **FormatDiffTable_SmallDiff**: 3 in-place rows — a typical incremental update. (~2555 ns/op)
- **FormatDiffTable_LargeDiff**: 10 in-place rows — stress-tests `strings.Builder` pre-allocation sizing. (~5502 ns/op)

### By Change Mix

- **FormatDiffTable_MixedCategories**: 5 rows across all three severity levels (2 recreate, 1 reboot, 2 in-place). (~3775 ns/op)

### Column Width

- **FormatDiffTable_WideValues**: 2 rows with long field names and values that exceed column header widths, exercising dynamic column-width computation. (~2092 ns/op)

## Baseline Results

Approximate baseline on a typical CI runner — `ns/op` (time/op) varies by hardware and load, while `B/op` and `allocs/op` primarily vary with Go version, architecture, and code changes:

| Benchmark                       | ns/op | B/op | allocs/op |
|---------------------------------|------:|-----:|----------:|
| FormatDiffTable_SingleChange    |  1359 |  800 |        16 |
| FormatDiffTable_SmallDiff       |  2555 | 1504 |        22 |
| FormatDiffTable_MixedCategories |  3775 | 2048 |        28 |
| FormatDiffTable_LargeDiff       |  5502 | 3312 |        40 |
| FormatDiffTable_WideValues      |  2092 | 1120 |        18 |

## Performance Notes

- `formatDiffTable` uses `strings.Builder` with `Grow()` pre-allocation to reduce reallocations; the pre-allocation scales with row count.
- Column widths are computed dynamically by scanning all rows, so wider field names or values increase runtime proportionally.
- Allocation count scales linearly with row count (each row appends to the builder and formats strings).
