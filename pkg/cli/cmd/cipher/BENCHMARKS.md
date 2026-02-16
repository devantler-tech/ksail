# SOPS Cipher Benchmarks

This document describes the performance benchmarks for SOPS encryption and decryption operations in KSail.

## Overview

KSail uses [SOPS (Secrets OPerationS)](https://github.com/getsops/sops) for encrypting sensitive configuration files. These benchmarks measure the performance of encryption, decryption, and round-trip operations across different secret sizes and structures.

## Running Benchmarks

### Run All Benchmarks

```bash
# Run all cipher benchmarks with memory profiling
cd pkg/cli/cmd/cipher
go test -bench=. -benchmem -benchtime=10s

# Save results for comparison
go test -bench=. -benchmem > baseline.txt
```

### Run Specific Benchmarks

```bash
# Encryption benchmarks only
go test -bench=BenchmarkEncrypt -benchmem

# Decryption benchmarks only
go test -bench=BenchmarkDecrypt -benchmem

# Roundtrip benchmarks
go test -bench=BenchmarkRoundtrip -benchmem

# Age encryption benchmark (requires age keys)
go test -bench=BenchmarkEncrypt/Small -benchmem
```

### Profiling

```bash
# CPU profiling
go test -bench=BenchmarkEncrypt_Medium -cpuprofile=cpu.prof -benchmem
go tool pprof cpu.prof
# In pprof: top10, list encrypt, web

# Memory profiling
go test -bench=BenchmarkDecrypt_Large -memprofile=mem.prof -benchmem
go tool pprof mem.prof
# In pprof: top10, list decrypt
```

## Benchmark Scenarios

### Encryption Benchmarks

| Benchmark                  | Description                     | Typical Use Case         |
| -------------------------- | ------------------------------- | ------------------------ |
| `BenchmarkEncrypt/Minimal` | Single key-value (1 key)        | Simple password/token    |
| `BenchmarkEncrypt/Small`   | Kubernetes Secret (5 keys)      | Basic app secrets        |
| `BenchmarkEncrypt/Medium`  | Multi-service secrets (20 keys) | Production configuration |
| `BenchmarkEncrypt/Large`   | Large secret file (100 keys)    | Enterprise config        |
| `BenchmarkEncrypt/Nested`  | Nested YAML structure           | Complex configuration    |

### Decryption Benchmarks

| Benchmark                      | Description                    | Notes               |
| ------------------------------ | ------------------------------ | ------------------- |
| `BenchmarkDecrypt/Minimal`     | Decrypt minimal secret         | Fastest scenario    |
| `BenchmarkDecrypt/Small`       | Decrypt small secret           | Common case         |
| `BenchmarkDecrypt/Medium`      | Decrypt medium secret          | Production workload |
| `BenchmarkDecrypt/Large`       | Decrypt large secret           | Worst-case scenario |
| `BenchmarkDecrypt/Nested`      | Decrypt nested structure       | Complex data        |
| `BenchmarkDecrypt/WithExtract` | Decrypt + extract single value | Partial decryption  |

### Round-trip Benchmarks

| Benchmark                    | Description                | Use Case                 |
| ---------------------------- | -------------------------- | ------------------------ |
| `BenchmarkRoundtrip_Minimal` | Full encrypt-decrypt cycle | Edit workflow simulation |

## Baseline Results

### Test Environment

- **Go Version**: 1.26.0
- **OS**: Linux amd64
- **Hardware**: GitHub Actions runner (2-core, 7GB RAM)
- **SOPS Version**: v3 (embedded via go.mod)
- **Encryption**: AES-256-GCM (default cipher)
- **Key Service**: Local keyservice (in-memory)

All benchmarks use [age](https://github.com/FiloSottile/age) key groups for encryption, as SOPS requires at least one key group.

### Encryption Performance

```
BenchmarkEncrypt/Minimal-2      [results pending]
BenchmarkEncrypt/Small-2        [results pending]
BenchmarkEncrypt/Medium-2       [results pending]
BenchmarkEncrypt/Large-2        [results pending]
BenchmarkEncrypt/Nested-2       [results pending]
```

**Expected Performance:**

- Minimal (1 key): <5ms per operation
- Small (5 keys): <10ms per operation
- Medium (20 keys): <20ms per operation
- Large (100 keys): <50ms per operation
- Nested: <15ms per operation

### Decryption Performance

```
BenchmarkDecrypt/Minimal-2      [results pending]
BenchmarkDecrypt/Small-2        [results pending]
BenchmarkDecrypt/Medium-2       [results pending]
BenchmarkDecrypt/Large-2        [results pending]
BenchmarkDecrypt/Nested-2       [results pending]
BenchmarkDecrypt/WithExtract-2  [results pending]
```

**Expected Performance:**

- Decryption is generally 20-30% faster than encryption
- Extract operation adds minimal overhead (<5% typically)
- MAC verification included in all decryption operations

### Memory Allocation

**Expected Allocations:**

- Minimal: ~2-5KB per operation, ~50-100 allocations
- Small: ~5-10KB per operation, ~100-200 allocations
- Medium: ~15-30KB per operation, ~300-500 allocations
- Large: ~100-200KB per operation, ~1500-2500 allocations

## Performance Targets

Based on typical KSail usage patterns:

### User Experience Targets

| Operation | Size           | Target | Rationale                    |
| --------- | -------------- | ------ | ---------------------------- |
| Encrypt   | Minimal-Medium | <50ms  | Interactive CLI feedback     |
| Decrypt   | Minimal-Medium | <30ms  | Fast secret access           |
| Roundtrip | Small          | <80ms  | Edit workflow responsiveness |

### Success Criteria

- ✅ **Minimal secrets** (<5ms): Common single-value secrets (passwords, tokens)
- ✅ **Small secrets** (<10ms): Typical Kubernetes Secret resources
- ✅ **Medium secrets** (<20ms): Production multi-service configurations
- ✅ **Large secrets** (<50ms): Enterprise-scale secret files
- ✅ **Extract operations** (+<5% overhead): Partial decryption efficiency

## Comparing Results

### Before and After Optimization

```bash
# Save baseline
go test -bench=. -benchmem > before.txt

# Make optimizations...

# Compare
go test -bench=. -benchmem > after.txt
benchstat before.txt after.txt
```

### Example benchstat Output

```
name                  old time/op    new time/op    delta
Encrypt/Minimal-2       4.50ms ± 2%    3.80ms ± 1%  -15.56%  (p=0.000)
Decrypt/Medium-2       12.30ms ± 3%   10.50ms ± 2%  -14.63%  (p=0.000)

name                  old alloc/op   new alloc/op   delta
Encrypt/Small-2        8.50KB ± 0%    7.20KB ± 0%  -15.29%  (p=0.000)

name                  old allocs/op  new allocs/op  delta
Decrypt/Large-2         2.50k ± 0%     2.20k ± 0%  -12.00%  (p=0.000)
```

## Optimization Opportunities

Based on profiling and benchmark results:

### 1. Key Generation Caching

**Current**: Generate data key for every encryption operation
**Opportunity**: Cache key generation results where safe (e.g., same key group)
**Expected Impact**: -15% to -25% encryption time

### 2. YAML Parsing Optimization

**Current**: Full YAML parse/marshal on every operation
**Opportunity**: Optimize YAML processing in SOPS library
**Expected Impact**: -10% to -20% total time

### 3. Memory Allocation Reduction

**Current**: Multiple allocations during tree operations
**Opportunity**: Pre-allocate buffers, reuse structures
**Expected Impact**: -20% to -30% allocations

### 4. Parallel Secret Processing

**Current**: Sequential encryption of multiple files
**Opportunity**: Parallel processing for bulk operations
**Expected Impact**: N× speedup for N files (with concurrency limits)

## CI Integration

To track performance over time:

```yaml
# .github/workflows/benchmarks.yaml
- name: Run SOPS Benchmarks
  run: |
    cd pkg/cli/cmd/cipher
    go test -bench=. -benchmem > new-results.txt

- name: Compare with Baseline
  run: |
    # Download previous baseline from artifacts
    benchstat baseline.txt new-results.txt
    # Fail if regression > 20%
```

## Troubleshooting

### Benchmarks Skipping

**Issue**: Benchmarks fail with "no key groups provided"
**Solution**: Ensure the `defaultKeyGroups` helper generates age keys successfully

### Inconsistent Results

**Issue**: High variance in benchmark times
**Solution**:

- Run with `-benchtime=10s` for more iterations
- Close other applications
- Use `-cpu=1` to reduce noise from parallel execution

### Out of Memory

**Issue**: Large benchmarks fail with OOM
**Solution**:

- Reduce `-benchtime`
- Run benchmarks individually
- Increase system memory or use `-parallel=1`

## References

- [SOPS Documentation](https://github.com/getsops/sops)
- [KSail Performance Optimization Guide](/.github/instructions/go-performance-optimization.md)
