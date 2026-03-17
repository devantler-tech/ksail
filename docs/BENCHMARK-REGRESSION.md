# Benchmark Regression Testing

KSail includes automated benchmark regression testing to detect performance changes in pull requests that modify Go code.

## How It Works

The [benchmark-regression](../.github/workflows/benchmark-regression.yaml) workflow runs automatically on PRs that change `**/*.go`, `go.mod`, or `go.sum` files. It:

1. Discovers packages that contain benchmark functions (avoids compiling the entire module)
2. Runs benchmarks on the PR branch and `main` **in parallel** (10 iterations, 1 s per benchmark)
3. Compares results with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) for statistical analysis
4. Posts (or updates) a comparison comment on the PR

## Interpreting Results

The PR comment groups results into three sections:

| Symbol | Label       | Meaning                                                              |
|--------|-------------|----------------------------------------------------------------------|
| 🔴     | Regression  | Statistically significant increase (p < 0.001, and ≥ 10% for sec/op) |
| 🟢     | Improvement | Statistically significant decrease (p < 0.001, and ≥ 10% for sec/op) |
| ⚪ ~    | Unchanged   | No significant change (p ≥ 0.001 or < 10% for sec/op)                |

Each row in the tables shows:

| Column        | Meaning                                                         | Goal               |
|---------------|-----------------------------------------------------------------|--------------------|
| **Benchmark** | Name of the benchmark function (without `Benchmark` prefix)     | —                  |
| **Metric**    | `sec/op`, `B/op`, or `allocs/op`                                | Lower is better    |
| **Change**    | Delta percentage vs `main`                                      | Negative is better |
| **p-value**   | Statistical confidence; < 0.001 means the change is significant | —                  |

The comment also includes a collapsed **Unchanged** section and a **Raw benchstat output** block for deeper inspection.

## Running Benchmarks Locally

```bash
# Run all benchmarks
go test -bench=. -benchmem -run=^$ ./...

# Run a specific package
go test -bench=. -benchmem -run=^$ ./pkg/k8s/readiness/...

# Compare before/after with benchstat
go test -bench=. -benchmem -count=10 -run=^$ ./... > before.txt
# (make changes)
go test -bench=. -benchmem -count=10 -run=^$ ./... > after.txt
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
| `pkg/client/argocd`         | `manager_bench_test.go` | ArgoCD GitOps client operations          |
| `pkg/client/flux`           | `client_bench_test.go`  | Flux GitOps client operations            |
| `pkg/client/helm`           | `client_bench_test.go`  | Helm client chart operations             |
| `pkg/client/kubectl`        | `client_bench_test.go`  | Kubectl command execution                |
| `pkg/client/kustomize`      | `client_bench_test.go`  | Kustomize build operations               |
| `pkg/k8s/readiness`         | `polling_bench_test.go` | Kubernetes resource polling              |

This table may not be exhaustive; additional benchmarks may exist in other packages.

See each package's `BENCHMARKS.md` for detailed baseline results and optimization opportunities.

## Install Duration Benchmarks

In addition to Go micro-benchmarks, KSail tracks real-world per-component install durations during `ksail cluster create` and `ksail cluster update` via the `--benchmark` flag. These cover Helm chart downloads, image pulls, and pod scheduling — workloads not suited to `go test -bench`.

Use `ksail cluster create --benchmark` or `ksail cluster update --benchmark` to capture install durations. Output is printed inline per component during the run.

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

**Benchmark times are inconsistent:** CI runners share hardware, so some variance is expected. The workflow uses strict thresholds (p < 0.001 and ≥ 10% delta for sec/op) to filter out runner noise. If you need higher confidence locally, increase `-count` or `-benchtime`.

**Workflow skipped:** The workflow only triggers on PRs that modify Go source files or module files.

## Future Enhancements

- **Threshold-based failure** — fail PRs that exceed a configurable regression limit
- **Baseline storage** — persist baseline results as artifacts for historical comparison
- **Performance trends** — track metrics across releases to detect gradual degradation
- **Selective benchmarking** — run only benchmarks for packages changed in the PR
