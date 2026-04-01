# Benchmark Regression Testing

KSail includes automated benchmark regression testing to detect performance changes in pull requests that modify Go code.

## How It Works

The [benchmark-regression](../.github/workflows/benchmark-regression.yaml) workflow runs automatically on PRs that change `**/*.go`, `go.mod`, or `go.sum` files. It:

1. Discovers packages that contain benchmark functions (avoids compiling the entire module)
2. Runs benchmarks on the PR branch and `main` **in parallel** (5 iterations, 500 ms per benchmark)
3. Compares results with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) for statistical analysis
4. **Fails the CI check** if statistically significant regressions are detected above the noise floor

## Regression Detection

The workflow gates on regressions using three layers of filtering:

| Layer | Filter | Purpose |
|-------|--------|---------|
| 1 | Mann-Whitney U test (p < 0.05) | Statistical significance — filters random variation |
| 2 | 20% noise floor (all metrics) | Filters CI runner variance — main and PR run on different machines |
| 3 | Sub-microsecond exclusion (sec/op only) | Skips nanosecond-range benchmarks where CPU clock jitter dominates |

A regression is only flagged when **all three layers** agree. This means only substantial, statistically confirmed performance degradations block the PR.

## Interpreting CI Failures

When the benchmark gate fails, the workflow logs show:

- The full `benchstat` comparison output (sec/op, B/op, allocs/op)
- A summary listing which benchmarks regressed and by how much
- A command to reproduce locally

## Running Benchmarks Locally

```bash
# Run all benchmarks
go test -bench=. -benchmem -run='^$' ./...

# Run a specific package
go test -bench=. -benchmem -run='^$' ./pkg/k8s/readiness/...

# Compare before/after with benchstat
go test -bench=. -benchmem -count=5 -run='^$' ./... > before.txt
# (make changes)
go test -bench=. -benchmem -count=5 -run='^$' ./... > after.txt
benchstat before.txt after.txt
```

Install `benchstat` if needed:

```bash
go install golang.org/x/perf/cmd/benchstat@v0.0.0-20260312031701-16a31bc5fbd0
```

## Writing Effective Benchmarks

Follow the conventions established in the existing benchmark files:

- Call `b.ReportAllocs()` to track allocations
- Use `b.ResetTimer()` after expensive setup
- Use `for range b.N` loop syntax (Go 1.22+)
- Use `b.TempDir()` for reproducible temp files
- Use table-driven scenarios to cover multiple input sizes
- Fail fast with `b.Fatalf` on unexpected errors
- Avoid `time.Sleep()` inside benchmark loops — measure real CPU work, not timers

## Troubleshooting

**Benchstat shows `~` for everything:** The change is within measurement noise, which is the expected result for most PRs that don't touch hot paths.

**Benchmark times are inconsistent:** CI runners share hardware, so some variance is expected. The workflow uses a 20% noise floor and statistical testing (p < 0.05) to filter runner noise. If you need higher confidence locally, increase `-count` or `-benchtime`.

**Workflow skipped:** The workflow only triggers on PRs that modify Go source files or module files.
