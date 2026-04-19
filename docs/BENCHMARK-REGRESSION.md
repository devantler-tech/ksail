# Benchmark Regression Testing

KSail includes automated benchmark regression testing to detect performance changes in pull requests that modify Go code.

## How It Works

The benchmark jobs in the [CI workflow](../.github/workflows/ci.yaml) run on pushes to `main` and pull requests. They use [`benchmark-action/github-action-benchmark`](https://github.com/benchmark-action/github-action-benchmark) for regression detection and historical tracking.

1. Discovers packages that contain benchmark functions (avoids compiling the entire module)
2. Runs benchmarks on the current branch
3. Compares results against the stored baseline (persisted in the [`benchmark-data` branch](https://github.com/devantler-tech/ksail/tree/benchmark-data))
4. **Fails the CI check** if a benchmark regresses beyond the configured threshold

On pushes to `main`, benchmark results are auto-pushed to the `benchmark-data` branch as the new baseline. On pull requests, results are compared against the baseline without updating it.

## Regression Detection

The workflow uses threshold-based regression detection:

| Setting           | Value | Meaning                                                                                              |
|-------------------|-------|------------------------------------------------------------------------------------------------------|
| `alert-threshold` | 150%  | Marks benchmarks as regressed and posts a PR comment when ≥1.5× slower than baseline                 |
| `fail-threshold`  | 200%  | Fails CI on non-PR runs (pushes to `main`, merge queue) when a benchmark is ≥2× slower than baseline |

On pull requests, the benchmark gate is **informational only**: results are posted as a PR comment when the alert threshold is exceeded, but CI never blocks on it. This is intentional — shared GitHub Actions runners have hardware variance of 2–5× between runs, making per-PR blocking gates unreliable. Real regressions are caught on pushes to `main`.

On push or merge-queue events, `fail-threshold` is active: a ≥2× regression fails CI, protecting `main` from persistent algorithmic regressions.

## Historical Results

Benchmark results are recorded in every [CI workflow run summary](https://github.com/devantler-tech/ksail/actions/workflows/ci.yaml). On pushes to `main`, the action auto-pushes updated results to the [`benchmark-data` branch](https://github.com/devantler-tech/ksail/tree/benchmark-data) (in `dev/bench/data.js`), following the [branch strategy recommended by `benchmark-action/github-action-benchmark`](https://github.com/benchmark-action/github-action-benchmark#charts-on-github-pages-1). The docs site fetches this data at build time to render performance trend charts.

## Interpreting CI Failures

When the benchmark gate fails, the workflow logs and PR comment show which benchmarks regressed and by how much relative to the stored baseline.

## Running Benchmarks Locally

```bash
# Run all benchmarks
go test -bench=. -benchmem -run='^$' ./...

# Run a specific package
go test -bench=. -benchmem -run='^$' ./pkg/k8s/readiness/...
```

## Writing Effective Benchmarks

Follow the conventions established in the existing benchmark files:

- Call `b.ReportAllocs()` to track allocations
- Use `b.ResetTimer()` after expensive setup
- Use `for b.Loop()` loop syntax (Go 1.26+)
- Use `b.TempDir()` for reproducible temp files
- Use table-driven scenarios to cover multiple input sizes
- Fail fast with `b.Fatalf` on unexpected errors
- Avoid `time.Sleep()` inside benchmark loops — measure real CPU work, not timers
- For I/O-bound benchmarks (e.g. tarball creation), run a warmup iteration before `b.ResetTimer()` to prime the OS page cache

## Troubleshooting

**No baseline data yet:** The first push to `main` after enabling the workflow auto-pushes the initial baseline to the `benchmark-data` branch. PRs opened before that will skip the comparison.

**Benchmark times are inconsistent:** CI runs each benchmark 5 times (`-count=5`). The 5 samples are averaged into a single representative value before comparison, giving a stable 1:1 comparison against the stored baseline. On pull requests, the benchmark gate is informational only — shared CI runners can vary 2–5× in hardware speed between runs, so per-PR blocking would produce too many false positives. I/O-bound benchmarks (`BenchmarkCreateTarball_*`) are excluded from the regression gate entirely since their timing is dominated by disk-cache state rather than algorithmic complexity.

**Benchmark jobs skipped:** The workflow runs on all PRs, but benchmark jobs are skipped when no Go source files (`**/*.go`, `go.mod`, `go.sum`) changed. In the merge queue, benchmark jobs are always skipped.
