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
go test -bench=. -benchmem ./pkg/k8s/...
```

### Run with longer benchmark time for more accurate results
```bash
go test -bench=. -benchmem -benchtime=5s ./pkg/k8s/...
```

### Run specific benchmark
```bash
go test -bench=BenchmarkWaitForMultipleResources_Sequential -benchmem ./pkg/k8s/...
```

### Save baseline results for comparison
```bash
go test -bench=. -benchmem ./pkg/k8s/... > baseline.txt
```

## Comparing Performance Changes

Use `benchstat` to compare before/after performance:

```bash
# Install benchstat if not already installed
go install golang.org/x/perf/cmd/benchstat@latest

# Run baseline
go test -bench=. -benchmem ./pkg/k8s/... > before.txt

# Make your changes...

# Run after changes
go test -bench=. -benchmem ./pkg/k8s/... > after.txt

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

## Baseline Results (Initial Run)

```
BenchmarkWaitForMultipleResources_Sequential/1_resource-4           441916    8529 ns/op    9227 B/op    57 allocs/op
BenchmarkWaitForMultipleResources_Sequential/5_resources-4          108722   33026 ns/op   39431 B/op   209 allocs/op
BenchmarkWaitForMultipleResources_Sequential/10_resources-4          53956   65453 ns/op   78078 B/op   398 allocs/op
BenchmarkWaitForMultipleResources_Sequential/20_resources-4          27798  131117 ns/op  155388 B/op   771 allocs/op
BenchmarkWaitForMultipleResources_MixedTypes/2d_2ds-4               130659   27412 ns/op   32260 B/op   173 allocs/op
BenchmarkWaitForMultipleResources_MixedTypes/5d_5ds-4                55470   65139 ns/op   77564 B/op   397 allocs/op
BenchmarkWaitForMultipleResources_MixedTypes/10d_10ds-4              27722  130215 ns/op  154880 B/op   774 allocs/op
BenchmarkWaitForMultipleResources_RealWorldCNI-4                    272376   14662 ns/op   14682 B/op    96 allocs/op
BenchmarkPollForReadiness_SingleCheck-4                            3635716     989 ns/op     688 B/op    11 allocs/op
```

**Platform:** AMD EPYC 7763 64-Core Processor (GitHub Actions runner)  
**Go Version:** 1.25.6  
**Date:** 2026-02-08

## Performance Insights

### Current Sequential Implementation
- **Linear scaling:** Time scales linearly with resource count (~6.5μs per resource)
- **Memory efficiency:** ~7.7KB per resource with ~38 allocations per resource
- **Real-world performance:** Cilium installation (~15μs total) is optimistic due to fake clientset

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

1. Run benchmarks before changes: `go test -bench=. -benchmem ./pkg/k8s/... > before.txt`
2. Make your changes
3. Run benchmarks after: `go test -bench=. -benchmem ./pkg/k8s/... > after.txt`
4. Compare: `benchstat before.txt after.txt`
5. Include results in PR description

Aim for:
- ✅ No regression in any scenario
- ✅ At least 10% improvement in target scenario
- ✅ No significant memory increase
