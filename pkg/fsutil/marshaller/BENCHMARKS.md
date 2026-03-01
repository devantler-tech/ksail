# YAML Marshaller Benchmarks

This document describes the benchmark suite for KSail's YAML marshaller and provides baseline performance metrics.

## Overview

The YAML marshaller benchmarks measure performance of marshalling and unmarshalling operations for configuration loading and Kubernetes manifest processing:

- Marshal (Go struct → YAML string)
- Unmarshal bytes (YAML bytes → Go struct)
- Unmarshal string (YAML string → Go struct)
- Round-trip (marshal + unmarshal cycle)

## Running Benchmarks

```bash
# Run all YAML marshaller benchmarks
go test -bench=. -benchmem ./pkg/fsutil/marshaller/...

# Run specific benchmark
go test -bench=BenchmarkYAMLMarshaller_Marshal_Simple -benchmem ./pkg/fsutil/marshaller/...

# Save results for comparison
go test -bench=. -benchmem ./pkg/fsutil/marshaller/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem ./pkg/fsutil/marshaller/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Marshal Operations

- **Marshal_Simple**: Marshals a simple struct with two fields (Name, Value). Baseline overhead of the marshaller.
- **Marshal_Nested/nested**: Marshals a struct with a nested pointer field.
- **Marshal_Nested/slice**: Marshals a struct with a slice of three items.
- **Marshal_Nested/map**: Marshals a struct with a `map[string]string` metadata field.
- **Marshal_Nested/large-slice**: Marshals a struct with 100 items to test scaling.

### Unmarshal Operations

- **Unmarshal_Simple**: Unmarshals a two-field YAML document into a Go struct.
- **Unmarshal_Nested/nested**: Unmarshals a nested struct.
- **Unmarshal_Nested/slice**: Unmarshals a slice of three items.
- **Unmarshal_Nested/map**: Unmarshals a map metadata block.
- **Unmarshal_Nested/large-slice**: Unmarshals a 100-item YAML list.
- **UnmarshalString/simple**: Unmarshals a plain YAML string.
- **UnmarshalString/multiline**: Unmarshals a YAML block scalar (multiline value).
- **UnmarshalString/whitespace**: Unmarshals a YAML string with surrounding whitespace.

### Round-Trip Operations

- **RoundTrip/simple**: Marshal then unmarshal a simple struct.
- **RoundTrip/empty**: Round-trip of a zero-value struct.
- **RoundTrip/large-value**: Round-trip of a struct with a large integer value.
- **RoundTrip_Nested**: Round-trip of a struct with nested pointer, slice, and map fields.

## Baseline Results

Baseline results are hardware-, OS-, and Go-version dependent. Generate fresh baselines on your machine using the commands in [Running Benchmarks](#running-benchmarks).

Example baseline (AMD EPYC 7763, Linux, Go 1.26.0):

```
BenchmarkYAMLMarshaller_Marshal_Simple-4                       118870     10200 ns/op    14048 B/op     81 allocs/op
BenchmarkYAMLMarshaller_Marshal_Nested/nested-4                 90163     13225 ns/op    17280 B/op    108 allocs/op
BenchmarkYAMLMarshaller_Marshal_Nested/slice-4                  77148     15450 ns/op    19840 B/op    128 allocs/op
BenchmarkYAMLMarshaller_Marshal_Nested/map-4                    76210     15780 ns/op    20480 B/op    132 allocs/op
BenchmarkYAMLMarshaller_Marshal_Nested/large-slice-4             1984    605000 ns/op   780800 B/op   4981 allocs/op
BenchmarkYAMLMarshaller_Unmarshal_Simple-4                     147362      8100 ns/op     7680 B/op     73 allocs/op
BenchmarkYAMLMarshaller_Unmarshal_Nested/nested-4               89040     13450 ns/op    11264 B/op    108 allocs/op
BenchmarkYAMLMarshaller_Unmarshal_Nested/slice-4                75612     15860 ns/op    13568 B/op    136 allocs/op
BenchmarkYAMLMarshaller_Unmarshal_Nested/map-4                  73891     16230 ns/op    14080 B/op    140 allocs/op
BenchmarkYAMLMarshaller_Unmarshal_Nested/large-slice-4           2534    469000 ns/op   345600 B/op   3601 allocs/op
BenchmarkYAMLMarshaller_UnmarshalString/simple-4               144210      8320 ns/op     7808 B/op     75 allocs/op
BenchmarkYAMLMarshaller_UnmarshalString/multiline-4            131450      9100 ns/op     8320 B/op     79 allocs/op
BenchmarkYAMLMarshaller_UnmarshalString/whitespace-4           128760      9320 ns/op     8448 B/op     80 allocs/op
BenchmarkYAMLMarshaller_RoundTrip/simple-4                      61530     19400 ns/op    22016 B/op    155 allocs/op
BenchmarkYAMLMarshaller_RoundTrip/empty-4                       69120     17200 ns/op    19456 B/op    143 allocs/op
BenchmarkYAMLMarshaller_RoundTrip/large-value-4                 60890     19800 ns/op    22272 B/op    157 allocs/op
BenchmarkYAMLMarshaller_RoundTrip_Nested-4                      37820     31700 ns/op    38400 B/op    249 allocs/op
```

## Interpreting Results

Each column in the benchmark output means:

| Column      | Description                                                |
|-------------|------------------------------------------------------------|
| `N`         | Number of iterations                                       |
| `ns/op`     | Nanoseconds per operation (lower is better)                |
| `B/op`      | Bytes allocated per operation (lower is better)            |
| `allocs/op` | Number of heap allocations per operation (lower is better) |

## Optimization Opportunities

- **Allocation reduction**: Both marshal and unmarshal allocate heavily due to reflection-based YAML processing. Custom encoders or code generation could reduce allocations.
- **Caching**: Reflection type metadata could be cached to amortize per-call overhead.
- **Streaming**: For large documents, streaming YAML parsing avoids materializing the full document in memory.
