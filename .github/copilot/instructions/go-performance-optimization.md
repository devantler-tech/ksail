# Go Performance Optimization Guide for KSail

This guide covers performance optimization techniques for KSail's Go codebase, focusing on measurement, profiling, and improvement strategies.

## Quick Reference

### Benchmarking

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkClusterCreate -benchmem ./pkg/svc/provisioner/cluster/...

# Run with CPU profiling
go test -bench=. -cpuprofile=cpu.prof -benchmem ./...

# Run with memory profiling
go test -bench=. -memprofile=mem.prof -benchmem ./...

# Compare before/after
go test -bench=. -benchmem ./... > before.txt
# Make changes...
go test -bench=. -benchmem ./... > after.txt
benchstat before.txt after.txt
```

### Profiling

```bash
# CPU profiling
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
# In pprof: top10, list <function>, web

# Memory profiling
go test -memprofile=mem.prof ./...
go tool pprof mem.prof
# In pprof: top10, list <function>

# Profile running binary
go build -o ksail
./ksail cluster create --cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Performance Optimization Areas

### 1. Algorithm and Data Structure Optimization

**Hot Paths to Profile:**

- Cluster provisioning (Kind, K3d, Talos)
- YAML parsing and validation
- Kubectl/Helm operations
- SOPS encryption/decryption
- Manifest processing pipelines

**Common Patterns:**

```go
// ❌ Inefficient: Multiple allocations
func processManifests(files []string) []Manifest {
    var result []Manifest
    for _, file := range files {
        data, _ := os.ReadFile(file)
        m := parseManifest(string(data))  // Unnecessary conversion
        result = append(result, m)        // Potential reallocation
    }
    return result
}

// ✅ Efficient: Pre-allocate, avoid conversions
func processManifests(files []string) []Manifest {
    result := make([]Manifest, 0, len(files))  // Pre-allocate
    for _, file := range files {
        data, _ := os.ReadFile(file)
        m := parseManifest(data)  // Parse bytes directly
        result = append(result, m)
    }
    return result
}
```

### 2. Memory Optimization

**Allocation Reduction:**

```go
// Use sync.Pool for frequently allocated objects
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func processLargeData() {
    buf := bufferPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        bufferPool.Put(buf)
    }()
    // Use buf...
}
```

**String Optimization:**

```go
// ❌ Inefficient: Multiple string concatenations
func buildCommand(parts []string) string {
    result := ""
    for _, part := range parts {
        result += part + " "
    }
    return result
}

// ✅ Efficient: Use strings.Builder
func buildCommand(parts []string) string {
    var b strings.Builder
    b.Grow(estimatedSize(parts))  // Pre-allocate
    for i, part := range parts {
        if i > 0 {
            b.WriteByte(' ')
        }
        b.WriteString(part)
    }
    return b.String()
}
```

### 3. Concurrency Optimization

**Parallel Processing:**

```go
// Process multiple clusters/workloads in parallel
func applyWorkloads(workloads []Workload) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(workloads))
    
    // Limit concurrency to avoid overwhelming system
    sem := make(chan struct{}, runtime.GOMAXPROCS(0))
    
    for _, w := range workloads {
        wg.Add(1)
        go func(workload Workload) {
            defer wg.Done()
            sem <- struct{}{}        // Acquire
            defer func() { <-sem }() // Release
            
            if err := workload.Apply(); err != nil {
                errChan <- err
            }
        }(w)
    }
    
    wg.Wait()
    close(errChan)
    
    // Collect errors
    var errs []error
    for err := range errChan {
        errs = append(errs, err)
    }
    return errors.Join(errs...)
}
```

### 4. I/O Optimization

**Docker API Optimization:**

```go
// Batch Docker API calls where possible
// Use connection pooling
// Cache frequently accessed data

// ❌ Multiple API calls
for _, container := range containers {
    info, _ := dockerClient.ContainerInspect(ctx, container.ID)
    // Process info...
}

// ✅ Single API call with filters
list, _ := dockerClient.ContainerList(ctx, container.ListOptions{
    Filters: filters.NewArgs(
        filters.Arg("label", "ksail.cluster"),
    ),
})
```

## Writing Benchmarks

### Basic Benchmark

```go
func BenchmarkYAMLParse(b *testing.B) {
    data := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: test
spec:
  containers:
  - name: test
    image: nginx`)
    
    b.ResetTimer()  // Reset timer after setup
    for i := 0; i < b.N; i++ {
        _, err := parseYAML(data)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

### Benchmark with Sub-benchmarks

```go
func BenchmarkClusterCreate(b *testing.B) {
    distributions := []string{"Vanilla", "K3s", "Talos"}
    
    for _, dist := range distributions {
        b.Run(dist, func(b *testing.B) {
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                // Create cluster...
            }
        })
    }
}
```

## Measurement Strategy

### 1. Establish Baselines

```bash
# Create baseline file
go test -bench=. -benchmem ./... > baseline.txt

# After optimization
go test -bench=. -benchmem ./... > optimized.txt

# Compare
benchstat baseline.txt optimized.txt
```

### 2. Profile Before Optimizing

```bash
# Always profile to identify real bottlenecks
go test -bench=BenchmarkSlow -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

### 3. Test with Realistic Data

- Use real-world cluster sizes
- Test with typical manifest counts
- Simulate network latency for remote APIs
- Test concurrent operations

## Common Anti-Patterns to Avoid

### ❌ Premature Optimization

Don't optimize without measurement. Profile first, then optimize hot paths.

### ❌ Over-Optimization

Don't sacrifice readability for micro-optimizations unless profiling shows significant impact.

### ❌ Ignoring GC Pressure

Watch allocation rates. High allocation = more GC pauses.

### ❌ Unbounded Concurrency

Always limit concurrent operations to prevent resource exhaustion.

## Performance Checklist

- [ ] Benchmark added for performance-critical code
- [ ] Profiling completed to identify bottlenecks
- [ ] Memory allocations minimized in hot paths
- [ ] String concatenations use strings.Builder
- [ ] Concurrent operations have proper limits
- [ ] Docker API calls batched where possible
- [ ] Large data structures pre-allocated
- [ ] Benchstat comparison shows improvement
- [ ] No regression in other benchmarks
- [ ] Code remains readable and maintainable

## Success Metrics

**Target Improvements:**

- Cluster creation time: -20% from baseline
- Memory allocations in hot paths: -30% from baseline
- Unit test suite: <30s total runtime
- Binary startup time: <100ms

**Measurement:**

- Use `benchstat` for statistical comparison
- Run benchmarks 5+ times for reliability
- Report both time and memory metrics
- Track improvement over time in CI
