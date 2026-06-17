# Benchmark Regression Testing

KSail includes automated benchmark regression testing to detect performance changes in pull requests that modify Go code.

## How It Works

The benchmark jobs in the [CI workflow](../.github/workflows/ci.yaml) run on pushes to `main` and pull requests. They use [`benchmark-action/github-action-benchmark`](https://github.com/benchmark-action/github-action-benchmark) for regression detection and historical tracking.

1. Discovers packages that contain benchmark functions (avoids compiling the entire module)
2. Runs benchmarks on the current branch (`-count=1` on pull requests, `-count=5` on pushes to `main`)
3. Compares the **deterministic allocation metrics** (`B/op`, `allocs/op`) against the stored baseline (persisted in the [`benchmark-data` branch](https://github.com/devantler-tech/ksail/tree/benchmark-data))
4. **Fails the pull-request check** if an allocation metric regresses beyond the threshold

Wall-clock `ns/op` is still recorded for the historical chart, but it is **never** used to gate — see [Regression Detection](#regression-detection) for why.

On pushes to `main`, the full results (including `ns/op`) are auto-pushed to the `benchmark-data` branch as the new baseline. On pull requests, results are compared against the baseline without updating it.

## Regression Detection

Shared GitHub-hosted runners have wall-clock variance of 2–5× between runs, so gating on `ns/op` produces false-positive "Performance Alert" comments on changes that never touch the benchmarked code. The gate therefore compares **only the deterministic metrics** — `allocs/op` and `B/op` — which depend on the code path, not on CPU speed or thermal throttling. A real algorithmic regression shows up in allocations; runner jitter does not.

Mechanically, the `🔍 Prepare benchmark regression gate input` step writes two files:

- `bench-filtered.txt` — the real measurements (including `ns/op`), used to update the baseline and the history chart.
- `bench-compare.txt` — the same data with `ns/op` pinned to `1`, used as the gate input. `github-action-benchmark`'s `go` parser turns each line into one comparable series per metric; pinning `ns/op` makes its series compare as ~0× of the baseline (so it can never alert), while the `B/op` and `allocs/op` series keep their real values **and byte-identical names** and still compare 1:1.

| Setting           | Value | Meaning                                                            |
|-------------------|-------|-------------------------------------------------------------------|
| `alert-threshold` | 150%  | A deterministic metric is flagged when it is ≥1.5× the baseline    |
| `fail-threshold`  | 150%  | Threshold above which the gate fails the check (pull requests only) |

- **On pull requests**, a ≥1.5× regression in `allocs/op` or `B/op` posts a comment **and fails the check**, blocking the merge. Because the gated metrics are jitter-free, this gate is reliable — it won't fire on runner noise.
- **On pushes to `main`**, the gate never fails: the `📤 Store Benchmark Data` job simply updates the baseline (and the `ns/op` chart). An intentional allocation increase that lands on `main` therefore re-baselines itself, so it can neither wedge `main` nor block the auto-release.

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

**Benchmark times are inconsistent:** This is expected on shared CI runners (2–5× variance between runs) and is exactly why the gate ignores `ns/op` and compares only the deterministic `allocs/op`/`B/op` metrics (see [Regression Detection](#regression-detection)). CI runs each benchmark once on pull requests (`-count=1`, since allocation metrics need no averaging) and 5 times on pushes to `main` (`-count=5`, to smooth the `ns/op` history chart), averaging the samples into one representative value per benchmark. I/O-bound benchmarks (`BenchmarkCreateTarball_*`) and sub-100 `ns/op` benchmarks are excluded from the gate entirely, since their timing is dominated by disk-cache state or clock jitter rather than algorithmic complexity.

**Benchmark jobs skipped:** The workflow runs on all PRs, but benchmark jobs are skipped unless a file in one of the 16 packages that contain `Benchmark*` functions, `go.mod`, `go.sum`, or `.github/workflows/ci.yaml` changed. PRs that only touch unrelated Go code (e.g. a new CLI command, documentation, or a package not in the benchmark filter) will skip benchmarks entirely. In the merge queue, benchmark jobs are always skipped.
