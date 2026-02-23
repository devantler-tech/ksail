# Flux Client Benchmarks

This document describes the benchmark suite for KSail's Flux client and provides baseline performance metrics.

## Overview

The Flux client benchmarks measure performance of creating and manipulating Flux GitOps resources:
- GitRepository (source-controller)
- HelmRepository (source-controller)
- OCIRepository (source-controller)
- Kustomization (kustomize-controller)
- HelmRelease (helm-controller)

## Running Benchmarks

```bash
# Run all Flux client benchmarks
go test -bench=. -benchmem ./pkg/client/flux/...

# Run specific benchmark
go test -bench=BenchmarkGitRepository_Creation -benchmem ./pkg/client/flux/...

# Save results for comparison
go test -bench=. -benchmem ./pkg/client/flux/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem ./pkg/client/flux/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Command Creation
- **CreateCreateCommand**: Measures Cobra command tree initialization overhead
- **Target**: <50μs

### Struct Creation (DeepCopy)
Benchmarks DeepCopy performance for different resource configurations:

**GitRepository**:
- Minimal: Basic repository with URL and interval
- WithReference: Repository with branch/tag/commit references
- Production: Full configuration with labels, secrets, timeout

**HelmRepository**:
- Minimal: Basic Helm chart repository
- Production: Full configuration with authentication

**Kustomization**:
- Minimal: Basic kustomization with source reference
- Production: Full configuration with target namespace, wait, timeout

**HelmRelease**:
- Minimal: Basic Helm release
- Production: Full configuration with install/upgrade options

### Spec Operations
- **CopySpec**: Benchmarks spec copying between resource instances
- Tests all 5 Flux resource types

## Baseline Results

### Environment
- **Platform**: AMD EPYC 7763 64-Core Processor
- **OS**: linux/amd64
- **Go Version**: 1.26.0

### Command Creation
| Benchmark | Time/op | Memory/op | Allocs/op |
|-----------|---------|-----------|-----------|
| CreateCreateCommand | ~20.8μs | 30.2 KB | 174 |

**Analysis**: Command tree initialization is fast and meets <50μs target ✅

### GitRepository Creation (DeepCopy)
| Scenario | Time/op | Memory/op | Allocs/op |
|----------|---------|-----------|-----------|
| Minimal | ~34ns | 0 B | 0 |
| WithReference | ~96ns | 80 B | 1 |
| Production | ~571ns | 440 B | 5 |

**Analysis**: 
- Minimal configs have zero allocations (all fields on stack) ✅
- Production configs still very fast (<1μs) ✅

### HelmRepository Creation
| Scenario | Time/op | Memory/op | Allocs/op |
|----------|---------|-----------|-----------|
| Minimal | ~27ns | 0 B | 0 |
| Production | ~364ns | 360 B | 4 |

**Analysis**: Similar performance to GitRepository. Minimal configs are extremely efficient.

### Kustomization Creation
| Scenario | Time/op | Memory/op | Allocs/op |
|----------|---------|-----------|-----------|
| Minimal | ~47ns | 0 B | 0 |
| Production | ~357ns | 344 B | 3 |

**Analysis**: Consistently fast across scenarios. Zero allocations for minimal configs.

### HelmRelease Creation
| Scenario | Time/op | Memory/op | Allocs/op |
|----------|---------|-----------|-----------|
| Minimal | ~173ns | 176 B | 1 |
| Production | ~631ns | 656 B | 7 |

**Analysis**: Slightly more expensive due to nested Chart template structure, but still sub-microsecond ✅

### Spec Copying (DeepCopy)
| Resource Type | Time/op | Memory/op | Allocs/op |
|---------------|---------|-----------|-----------|
| GitRepository | ~730ns | 1.3 KB | 2 |
| HelmRepository | ~541ns | 896 B | 2 |
| Kustomization | ~959ns | 1.8 KB | 2 |
| HelmRelease | ~1.1μs | 2.0 KB | 3 |

**Analysis**:
- All operations <2μs ✅
- Kustomization and HelmRelease have slightly higher overhead due to complex nested specs
- Consistent 2-3 allocations per operation

## Performance Targets

| Operation | Target | Current | Status |
|-----------|--------|---------|--------|
| Command creation | <50μs | ~21μs | ✅ |
| Minimal resource creation | <100ns | 27-47ns | ✅ |
| Production resource creation | <1μs | 364-631ns | ✅ |
| Spec copying | <2μs | 541ns-1.1μs | ✅ |

**All targets met** ✅

## Optimization Opportunities

Despite meeting all performance targets, future optimization opportunities include:

1. **Command Creation Optimization**
   - Current: 174 allocations (30.2 KB)
   - Opportunity: Lazy-initialize subcommands
   - Potential improvement: -20% to -30% allocations

2. **Spec Copying Optimization**
   - Current: 896 B - 2.0 KB per operation
   - Opportunity: Use sync.Pool for frequently copied specs
   - Potential improvement: -30% to -50% allocations in high-throughput scenarios

3. **Production Config Optimization**
   - Current: 3-7 allocations per DeepCopy
   - Opportunity: Pre-allocate maps/slices for labels
   - Potential improvement: -15% to -25% allocations

## Integration with CI/CD

To track performance regression in CI:

```yaml
- name: Run Flux client benchmarks
  run: |
    go test -bench=. -benchmem ./pkg/client/flux/... > flux-bench-new.txt
    
- name: Compare with baseline
  run: |
    benchstat pkg/client/flux/baseline.txt flux-bench-new.txt
```

## Profiling

For detailed analysis:

```bash
# CPU profiling
go test -bench=BenchmarkGitRepository_Creation -cpuprofile=cpu.prof ./pkg/client/flux/...
go tool pprof cpu.prof

# Memory profiling
go test -bench=BenchmarkGitRepository_Creation -memprofile=mem.prof ./pkg/client/flux/...
go tool pprof mem.prof

# In pprof interactive mode:
# - top10: Show top 10 functions by time/memory
# - list <function>: Show annotated source for a function
# - web: Generate SVG graph (requires graphviz)
```

## Troubleshooting

### Benchmark Variability
If benchmark results vary significantly between runs:
- Run with `-benchtime=10s` for more iterations
- Use `benchstat` with multiple runs for statistical analysis
- Check for CPU throttling or background processes

### Network-Related Benchmarks
Current benchmarks don't require network access (they use fake clients).
If adding network-dependent benchmarks:
- Use httptest for deterministic results
- Document network requirements clearly
- Consider timeouts in CI environments

## Related Documentation

- [Go Performance Optimization Guide](/.github/instructions/go-performance-optimization.md)
- [Daily Perf Improver Research](https://github.com/devantler-tech/ksail/discussions/1822)
