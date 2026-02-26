# Docker Client Benchmarks

This document describes the performance benchmarks for the Docker client wrapper and registry manager, which are critical components for cluster provisioning and registry operations.

## Overview

The Docker client benchmarks measure the performance of:

1. **Docker Engine Operations** - Client creation and API connection setup
2. **Registry Manager Operations** - Container registry lifecycle configuration

These operations are critical to KSail's cluster provisioning performance, particularly for:

- Vanilla (Kind) distribution - uses Docker containers for cluster nodes
- K3s (K3d) distribution - uses Docker containers for cluster nodes
- Registry mirror operations - pull-through caching for faster image pulls

## Running Benchmarks

### All Benchmarks

```bash
go test -bench=. -benchmem ./pkg/client/docker/
```

### Specific Benchmark

```bash
go test -bench=BenchmarkGetDockerClient -benchmem ./pkg/client/docker/
```

### With CPU Profiling

```bash
go test -bench=. -cpuprofile=cpu.prof -benchmem ./pkg/client/docker/
go tool pprof cpu.prof
```

### Comparing Before/After Changes

```bash
# Before changes
go test -bench=. -benchmem ./pkg/client/docker/ > before.txt

# Make your changes...

# After changes
go test -bench=. -benchmem ./pkg/client/docker/ > after.txt

# Compare (requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest)
benchstat before.txt after.txt
```

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent and can become stale quickly.
Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks).

Example baseline (AMD EPYC 7763, Linux, Go 1.26.0):

```
BenchmarkGetDockerClient-4                               780775       1499 ns/op     1784 B/op       23 allocs/op
BenchmarkGetConcreteDockerClient-4                       766017       1495 ns/op     1784 B/op       23 allocs/op
BenchmarkNewRegistryManager-4                          47744233         24.15 ns/op       16 B/op        1 allocs/op
BenchmarkNewRegistryManagerNilClient-4                 1000000000          0.6232 ns/op        0 B/op        0 allocs/op
BenchmarkBuildContainerConfig_Minimal-4                 2890894        414.9 ns/op      992 B/op        7 allocs/op
BenchmarkBuildContainerConfig_Production-4              1373319        911.6 ns/op     1196 B/op       17 allocs/op
BenchmarkBuildHostConfig_Minimal-4                      3678830        327.4 ns/op     1312 B/op        3 allocs/op
BenchmarkBuildNetworkConfig_Minimal-4                  397080200          3.026 ns/op        0 B/op        0 allocs/op
BenchmarkResolveVolumeName_Minimal-4                   143685614          8.118 ns/op        0 B/op        0 allocs/op
BenchmarkBuildProxyCredentialsEnv_WithCredentials-4     3380788        356.8 ns/op      161 B/op        9 allocs/op
BenchmarkBuildProxyCredentialsEnv_NoCredentials-4      428624280          2.832 ns/op        0 B/op        0 allocs/op
```

## Benchmarked Operations

### Docker Engine Operations

| Benchmark                          | Description                 | Typical Use Case                     |
|------------------------------------|-----------------------------|--------------------------------------|
| `BenchmarkGetDockerClient`         | Create Docker API client    | Every cluster provisioning operation |
| `BenchmarkGetConcreteDockerClient` | Create concrete client type | Advanced Docker API operations       |

### Registry Manager Operations

| Benchmark                                           | Description                  | Typical Use Case                      |
|-----------------------------------------------------|------------------------------|---------------------------------------|
| `BenchmarkNewRegistryManager`                       | Manager instantiation        | Once per cluster provisioning         |
| `BenchmarkNewRegistryManagerNilClient`              | Error validation             | Input validation overhead             |
| `BenchmarkBuildContainerConfig_Minimal`             | Basic container config       | Simple registry without auth          |
| `BenchmarkBuildContainerConfig_Production`          | Container config with auth   | Registry with upstream authentication |
| `BenchmarkBuildHostConfig_Minimal`                  | Host config (ports, volumes) | Every registry container              |
| `BenchmarkBuildNetworkConfig_Minimal`               | Network configuration        | Every registry container              |
| `BenchmarkResolveVolumeName_Minimal`                | Volume name resolution       | Every registry container              |
| `BenchmarkBuildProxyCredentialsEnv_WithCredentials` | Credential env vars          | Authenticated upstream registries     |
| `BenchmarkBuildProxyCredentialsEnv_NoCredentials`   | No-credential case           | Public upstream registries            |

## Performance Characteristics

### Ultra-Fast Operations (< 10ns)

- Network config (~3ns, 0 allocs) - extremely lightweight, essentially a struct copy
- No-credentials case (~2.8ns, 0 allocs) - fast path when auth not needed
- Nil client validation (~0.6ns, 0 allocs) - compiler-optimized error check

### Very Fast Operations (10-50ns)

- Volume name resolution (~8ns, 0 allocs) - simple string operation
- Registry manager creation (~24ns, 16B, 1 alloc) - minimal struct allocation

### Fast Operations (300-500ns)

- Host config (~327ns, 1312B, 3 allocs) - port/volume bindings
- Credential env (~357ns, 161B, 9 allocs) - environment variable expansion
- Minimal container config (~415ns, 992B, 7 allocs) - basic registry setup

### Moderate Operations (500-1000ns)

- Production container config (~912ns, 1196B, 17 allocs) - includes authentication setup

### Heavier Operations (1-2μs)

- Docker client creation (~1.5μs, 1784B, 23 allocs) - API version negotiation overhead

## Observations

1. **Docker Client Creation Dominates** - At ~1.5μs per call, Docker client creation is the slowest operation benchmarked, but this is unavoidable due to Docker API version negotiation.

2. **Configuration Builders Are Fast** - All container/host/network config builders complete in sub-microsecond times, making them suitable for hot paths.

3. **Authentication Adds Overhead** - Production config with credentials (~912ns) is 2.2x slower than minimal config (~415ns) due to environment variable expansion and validation.

4. **Registry Manager Is Lightweight** - Manager creation is nearly free (~24ns), allowing safe instantiation even in performance-sensitive code.

5. **Zero-Allocation Fast Paths** - Network config, volume name resolution, and no-credential cases have zero allocations, indicating highly optimized code paths.

## Optimization Opportunities

Based on these benchmarks:

1. **Docker Client Pooling** - The ~1.5μs Docker client creation time suggests pooling or caching clients could provide significant benefits if clients are frequently created/destroyed. However, clients are typically long-lived in KSail's architecture.

2. **Configuration Caching** - Container/host/network configs could be cached for repeated operations on the same registry configuration, though the sub-microsecond times suggest limited benefit unless operations are extremely frequent.

3. **Credential Handling** - The 2.5x overhead for credential processing could be reduced by:
   - Caching expanded environment variables
   - Pre-validating credentials once at config creation
   - Lazy evaluation only when credentials are actually used

4. **Allocation Reduction** - Production container config has 17 allocations (vs 7 for minimal). Profile to identify:
   - String concatenations that could use builders
   - Map allocations that could be pre-sized
   - Temporary structures that could be reused

## Performance Targets

Based on KSail's cluster provisioning goals:

- **Docker client creation**: < 2μs (current: ~1.5μs) ✅
- **Registry manager creation**: < 100ns (current: ~24ns) ✅
- **Configuration generation**: < 1μs per operation (current: 327-912ns) ✅
- **Memory efficiency**: Minimize allocations in hot paths

All current benchmarks meet or exceed performance targets. Future optimizations should focus on:

- Reducing allocations in production container config
- Caching strategies for frequently-used configurations
- Docker client lifecycle management

## Future Work

1. Add benchmarks for actual Docker API operations (with test containers)
2. Benchmark registry lifecycle operations (create, delete, health check)
3. Profile concurrent registry operations
4. Benchmark image pull and push operations
5. Measure performance with various registry configurations
6. Add benchmarks for registry cleanup and garbage collection
