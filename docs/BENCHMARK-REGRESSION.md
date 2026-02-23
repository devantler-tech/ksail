# Benchmark Regression Testing

KSail includes automated benchmark regression testing to detect performance changes in pull requests that modify Go code.

## How It Works

The [benchmark-regression](../.github/workflows/benchmark-regression.yaml) workflow runs automatically on PRs that change `**/*.go`, `go.mod`, or `go.sum` files. It:

1. Runs all Go benchmarks on the PR branch (5 iterations, 3 s per benchmark)
2. Runs the same benchmarks on `main` to establish a baseline
3. Compares results with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) for statistical analysis
4. Posts (or updates) a comparison comment on the PR

## Interpreting Results

`benchstat` output contains three key columns per benchmark:

| Column        | Meaning                        | Goal            |
|---------------|--------------------------------|-----------------|
| **sec/op**    | Time per operation             | Lower is better |
| **B/op**      | Bytes allocated per operation  | Lower is better |
| **allocs/op** | Heap allocations per operation | Lower is better |

Each row shows `old` (main) vs `new` (PR) with a delta percentage and a p-value:

- **p < 0.05** — statistically significant change (highlighted by `benchstat`)
- **~ (p ≥ 0.05)** — no significant change; difference is within measurement noise

A positive delta in sec/op or B/op indicates a regression; a negative delta indicates an improvement.

## Running Benchmarks Locally

```bash
# Run all benchmarks
go test -bench=. -benchmem -run=^$ ./...

# Run a specific package
go test -bench=. -benchmem -run=^$ ./pkg/k8s/readiness/...

# Compare before/after with benchstat
go test -bench=. -benchmem -count=5 -run=^$ ./... > before.txt
# (make changes)
go test -bench=. -benchmem -count=5 -run=^$ ./... > after.txt
benchstat before.txt after.txt
```

Install `benchstat` if needed:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
```

## Current Benchmark Coverage

| Package                     | File                    | What It Tests                            |
|-----------------------------|-------------------------|------------------------------------------|
| `pkg/apis/cluster/v1alpha1` | `marshal_bench_test.go` | YAML/JSON marshalling of cluster configs |
| `pkg/cli/cmd/cipher`        | `cipher_bench_test.go`  | SOPS encrypt/decrypt operations          |
| `pkg/k8s/readiness`         | `polling_bench_test.go` | Kubernetes resource polling              |

See each package's `BENCHMARKS.md` for detailed baseline results and optimization opportunities.

## Writing Effective Benchmarks

Follow the conventions established in the existing benchmark files:

- Call `b.ReportAllocs()` to track allocations
- Use `b.ResetTimer()` after expensive setup
- Use `for range b.N` loop syntax (Go 1.22+)
- Use `b.TempDir()` for reproducible temp files
- Use table-driven scenarios to cover multiple input sizes
- Fail fast with `b.Fatalf` on unexpected errors

## Troubleshooting

**Benchstat shows `~` for everything:** The change is within measurement noise, which is the expected result for most PRs that don't touch hot paths.

**Benchmark times are inconsistent:** CI runners share hardware, so some variance is expected. If you need higher confidence, increase `-count` or `-benchtime` when running locally.

**Workflow skipped:** The workflow only triggers on PRs that modify Go source files or module files.

## Future Enhancements

- **Threshold-based failure** — fail PRs that exceed a configurable regression limit
- **Baseline storage** — persist baseline results as artifacts for historical comparison
- **Performance trends** — track metrics across releases to detect gradual degradation
- **Selective benchmarking** — run only benchmarks for packages changed in the PR
