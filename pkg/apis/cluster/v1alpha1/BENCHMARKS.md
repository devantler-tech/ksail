# Cluster YAML Marshalling Benchmarks

This document describes the performance benchmarks for KSail cluster configuration marshalling operations.

## Overview

The benchmarks in `marshal_bench_test.go` measure the performance of cluster configuration marshalling, which is a critical operation that occurs when:

- Saving cluster configurations to `ksail.yaml`
- Displaying cluster information
- Serializing cluster state for storage

The marshalling implementation uses custom reflection-based logic to:
1. Prune default values to produce minimal YAML output
2. Convert Go structs to YAML/JSON representations
3. Handle special types (durations, enums, nested structs)

## Running Benchmarks

### All Marshalling Benchmarks

```bash
go test -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/...
```

### Specific Benchmark Suites

```bash
# YAML marshalling only
go test -bench=BenchmarkCluster_MarshalYAML -benchmem ./pkg/apis/cluster/v1alpha1/...

# JSON marshalling only
go test -bench=BenchmarkCluster_MarshalJSON -benchmem ./pkg/apis/cluster/v1alpha1/...

# Default pruning only
go test -bench=BenchmarkPruneClusterDefaults -benchmem ./pkg/apis/cluster/v1alpha1/...

# Full encoding (marshal + encode)
go test -bench=BenchmarkYAMLEncode -benchmem ./pkg/apis/cluster/v1alpha1/...
```

### Longer Runs for Accuracy

```bash
# Run each benchmark for 5 seconds to get more stable results
go test -bench=. -benchmem -benchtime=5s ./pkg/apis/cluster/v1alpha1/...
```

## Benchmark Scenarios

### BenchmarkCluster_MarshalYAML

Tests the `MarshalYAML()` method across different cluster configuration complexities:

1. **Minimal** - Default `NewCluster()` with minimal fields
2. **WithBasicConfig** - Basic distribution + provider
3. **WithCNI** - Adds CNI configuration
4. **WithGitOps** - Includes GitOps engine and workload spec
5. **FullProductionCluster** - Complete production configuration with all components

**What it measures:**
- Custom MarshalYAML implementation performance
- Default pruning overhead
- Reflection-based struct-to-map conversion

### BenchmarkCluster_MarshalJSON

Tests the `MarshalJSON()` method with varying configurations:

1. **Minimal** - Smallest possible cluster
2. **WithBasicConfig** - Basic configuration
3. **FullProductionCluster** - Fully-specified cluster

**What it measures:**
- JSON marshalling performance (used internally by YAML encoder)
- Default pruning impact on JSON output

### BenchmarkYAMLEncode

Tests full end-to-end YAML encoding using `yaml.Marshal()`:

1. **Minimal** - Minimal cluster configuration
2. **FullProductionCluster** - Complex production cluster

**What it measures:**
- Complete YAML encoding pipeline
- YAML library overhead
- Total time for marshalling + encoding

### BenchmarkJSONEncode

Tests standard `json.Marshal()` performance for comparison.

**What it measures:**
- Complete JSON encoding performance
- Baseline for comparison with custom marshalling

### BenchmarkPruneClusterDefaults

Tests the default pruning operation in isolation:

1. **MostlyDefaults** - Cluster with mostly default values
2. **MixedDefaultsAndCustom** - Mix of default and custom values
3. **AllCustomValues** - All fields set to non-default values

**What it measures:**
- Default pruning algorithm performance
- Reflection overhead for field traversal
- Impact of default value matching logic

## Performance Baselines

*Baseline results will be added after initial benchmark run*

Run benchmarks and save baseline:

```bash
go test -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/... > baselines/cluster-marshalling-baseline.txt
```

## Comparing Performance

After making optimizations, compare with baseline:

```bash
# Save new results
go test -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/... > new-results.txt

# Compare with benchstat (install if needed: go install golang.org/x/perf/cmd/benchstat@latest)
benchstat baselines/cluster-marshalling-baseline.txt new-results.txt
```

## Performance Targets

Based on typical usage patterns:

- **Cluster save operation**: Complete YAML marshalling should be <10ms
- **Memory allocations**: Keep total allocations <100KB for typical configs
- **Allocation count**: Aim for <500 allocations per marshal operation

## Optimization Opportunities

If benchmarks reveal performance issues, consider:

1. **Reduce reflection overhead**
   - Cache type information
   - Pre-compute default values
   - Use code generation instead of reflection

2. **Memory optimization**
   - Pre-allocate maps with estimated capacity
   - Reuse buffers with `sync.Pool`
   - Reduce intermediate allocations

3. **Algorithmic improvements**
   - Skip default pruning for fields known to be non-default
   - Use lookup tables instead of tag parsing
   - Parallelize independent operations (if beneficial)

## Related Files

- `marshal.go` - Custom marshalling implementation
- `types.go` - Cluster type definitions
- `defaults.go` - Default value logic
- `validation.go` - Validation logic (may affect marshalling)

## CI Integration

These benchmarks can be integrated into CI to detect performance regressions:

```yaml
- name: Run marshalling benchmarks
  run: |
    go test -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/... > new-bench.txt
    # Compare with baseline (fail if >20% regression)
    benchstat baseline.txt new-bench.txt
```

## Profiling

For deeper analysis, generate CPU and memory profiles:

```bash
# CPU profile
go test -bench=BenchmarkCluster_MarshalYAML -cpuprofile=cpu.prof ./pkg/apis/cluster/v1alpha1/...
go tool pprof cpu.prof

# Memory profile
go test -bench=BenchmarkCluster_MarshalYAML -memprofile=mem.prof ./pkg/apis/cluster/v1alpha1/...
go tool pprof mem.prof

# In pprof interactive mode:
# - top10: Show top 10 functions
# - list <function>: Show source code with annotations
# - web: Generate visual graph (requires Graphviz)
```

## Notes

- Benchmarks use `b.ReportAllocs()` to track memory allocation metrics
- Each scenario is run multiple times (controlled by `b.N`) to get statistically significant results
- The Go testing framework automatically adjusts `b.N` to run benchmarks for ~1 second each
- Use `-benchtime` flag to increase duration for more stable measurements
- Results may vary based on CPU, memory, and system load
