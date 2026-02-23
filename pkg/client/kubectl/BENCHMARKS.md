# Kubectl Client Benchmarks

This document describes the performance benchmarks for the kubectl client wrapper and how to use them.

## Overview

The kubectl client benchmarks measure the performance of command creation operations, which are critical to KSail's CLI performance. These operations happen frequently during command initialization and affect overall CLI responsiveness.

## Running Benchmarks

### All Benchmarks

````bash
go test -bench=. -benchmem ./pkg/client/kubectl/
````

### Specific Benchmark

````bash
go test -bench=BenchmarkCreateApplyCommand -benchmem ./pkg/client/kubectl/
````

### With CPU Profiling

````bash
go test -bench=. -cpuprofile=cpu.prof -benchmem ./pkg/client/kubectl/
go tool pprof cpu.prof
````

### Comparing Before/After Changes

````bash
# Before changes
go test -bench=. -benchmem ./pkg/client/kubectl/ > before.txt

# Make your changes...

# After changes
go test -bench=. -benchmem ./pkg/client/kubectl/ > after.txt

# Compare (requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest)
benchstat before.txt after.txt
````

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent and can become stale quickly.
Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks)
and record the results together with the relevant commit hash in your change log or performance notes.

For example:

````bash
go test -bench=. -benchmem ./pkg/client/kubectl/ > baseline.txt
````

## Benchmarked Operations

| Benchmark                        | Description               | Typical Use Case               |
|----------------------------------|---------------------------|--------------------------------|
| `BenchmarkCreateClient`          | Client instantiation      | Every command invocation       |
| `BenchmarkCreateApplyCommand`    | Apply command creation    | `ksail workload apply`         |
| `BenchmarkCreateGetCommand`      | Get command creation      | `ksail workload get`           |
| `BenchmarkCreateDeleteCommand`   | Delete command creation   | `ksail workload delete`        |
| `BenchmarkCreateDescribeCommand` | Describe command creation | `ksail workload describe`      |
| `BenchmarkCreateLogsCommand`     | Logs command creation     | `ksail workload logs`          |
| `BenchmarkCreateWaitCommand`     | Wait command creation     | `ksail workload wait`          |
| `BenchmarkCreateNamespaceCmd`    | Namespace generator       | `ksail workload gen namespace` |
| `BenchmarkCreateDeploymentCmd`   | Deployment generator      | `ksail workload gen deployment` |
| `BenchmarkCreateServiceCmd`      | Service generator         | `ksail workload gen service`   |

## Performance Characteristics

### Fast Operations (< 50µs)

- Client creation - essentially free, no allocations
- Wait command (~10µs, 92 allocs) - lightweight, minimal flags
- Delete command (~22µs, 121 allocs) - simple command structure
- Describe command (~22µs, 142 allocs) - similar to delete

### Moderate Operations (50-250µs)

- Get command (~46µs, 205 allocs) - more complex flag structure
- Apply command (~47µs, 311 allocs) - highest allocation count for basic commands
- Logs command (~40µs, 144 allocs) - streaming-related complexity
- Namespace generator (~250µs, 1561 allocs) - wraps kubectl create

### Observations

1. **Client creation is negligible** - The `NewClient` constructor has minimal allocation overhead
2. **Command creation varies widely** - From ~10µs (wait) to ~250µs (namespace gen)
3. **Allocations correlate with complexity** - More flags/features = more allocations
4. **Resource generators are heaviest** - Generator commands (~1500 allocs) vs basic commands (~100-300 allocs)

## Optimization Opportunities

Based on these benchmarks:

1. **Factory reuse** - The `createFactory` method creates a new factory for each command. Investigate if factories can be safely cached/reused.

2. **ConfigFlags optimization** - Each command creates new `ConfigFlags`. Consider pooling or reusing where safe.

3. **Command pooling** - For frequently used commands, consider caching initialized command structures.

4. **Generator command optimization** - The resource generator commands (namespace, deployment, service) have 5-10x more allocations than basic commands. Profile and optimize the `newResourceCmd` path.

## Performance Targets

Based on KSail's performance goals:

- **CLI startup**: < 100ms (includes all command initialization)
- **Command creation**: Should remain < 1ms for typical operations
- **Allocation reduction**: Target 20-30% reduction in hot paths

## Future Work

1. Add benchmarks for command execution (not just creation)
2. Benchmark with real cluster connections
3. Profile memory allocation patterns
4. Compare performance across different kubeconfig sizes/complexity
5. Benchmark concurrent command creation scenarios
