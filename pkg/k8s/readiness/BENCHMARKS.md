# Kubernetes Resource Polling Benchmarks

This directory contains Go benchmarks for the Kubernetes resource polling functionality in KSail.

## Purpose

These benchmarks establish performance baselines for:

- Multi-resource polling operations (sequential implementation)
- Mixed resource types (deployments and daemonsets)
- Real-world CNI installation scenarios
- Base polling mechanism overhead

## Running Benchmarks

### Run all benchmarks in this package

```bash
go test -bench=. -benchmem ./pkg/k8s/readiness/...
```

### Run with longer benchmark time for more accurate results

```bash
go test -bench=. -benchmem -benchtime=5s ./pkg/k8s/readiness/...
```

### Run specific benchmark

```bash
go test -bench=BenchmarkWaitForMultipleResources_Sequential -benchmem ./pkg/k8s/readiness/...
```

### Save baseline results for comparison

```bash
go test -bench=. -benchmem ./pkg/k8s/readiness/... > baseline.txt
```

## Comparing Performance Changes

Use `benchstat` to compare before/after performance:

```bash
# Install benchstat if not already installed
go install golang.org/x/perf/cmd/benchstat@latest

# Run baseline
go test -bench=. -benchmem ./pkg/k8s/readiness/... > before.txt

# Make your changes...

# Run after changes
go test -bench=. -benchmem ./pkg/k8s/readiness/... > after.txt

# Compare results
benchstat before.txt after.txt
```

## Benchmark Scenarios

### BenchmarkWaitForMultipleResources_Sequential

Tests the current sequential implementation with varying resource counts (1, 5, 10, 20 resources).

**Key Metrics:**

- Time per operation increases linearly with resource count
- Memory allocations scale with resources
- Baseline for future parallel implementation comparison

### BenchmarkWaitForMultipleResources_MixedTypes

Tests realistic scenarios with mixed deployments and daemonsets (2d+2ds, 5d+5ds, 10d+10ds).

**Key Metrics:**

- Representative of actual CNI and component installations
- Shows overhead of handling different resource types

### BenchmarkWaitForMultipleResources_RealWorldCNI

Simulates a typical Cilium CNI installation:

- cilium-operator deployment (2 replicas)
- cilium daemonset (3 nodes)
- coredns deployment (2 replicas)

**Key Metrics:**

- Most realistic benchmark for actual cluster operations
- Useful for measuring user-facing performance improvements

### BenchmarkPollForReadiness_SingleCheck

Measures the base polling mechanism overhead with immediate readiness.

**Key Metrics:**

- Minimum overhead per polling operation
- Useful for understanding fixed costs

## Baseline Results

Baselines should be generated after merging by running:

```bash
go test -bench=. -benchmem -count=5 ./pkg/k8s/readiness/... | tee baseline.txt
```

## Performance Insights

### Current Sequential Implementation

- Time scales linearly with resource count
- Memory allocations scale proportionally with resources
- Real-world benchmarks use a fake clientset so times reflect polling overhead only, not network I/O

### Optimization Opportunities

1. **Parallel polling:** Could reduce wait time significantly for multiple resources
2. **Connection pooling:** Reduce per-resource overhead
3. **Batch status checks:** Query multiple resources in single API call
4. **Allocation reduction:** Pre-allocate slices based on resource count

## Future Work

Based on these benchmarks, potential optimizations include:

1. **Parallel Resource Polling**
   - Estimated improvement: 50-70% for 5+ resources
   - Complexity: Medium (need timeout coordination)
   - Risk: Low (can run checks concurrently)

2. **Memory Optimization**
   - Pre-allocate result slices
   - Reuse context objects
   - Pool temporary buffers

3. **Smarter Timeout Handling**
   - Parallel execution with shared deadline
   - Fail-fast on first error option
   - Progress tracking

## Contributing

When making performance-related changes:

1. Run benchmarks before changes: `go test -bench=. -benchmem ./pkg/k8s/readiness/... > before.txt`
2. Make your changes
3. Run benchmarks after: `go test -bench=. -benchmem ./pkg/k8s/readiness/... > after.txt`
4. Compare: `benchstat before.txt after.txt`
5. Include results in PR description

Aim for:

- No regression in any scenario
- At least 10% improvement in target scenario
- No significant memory increase
