# Cluster YAML Marshalling Benchmarks

The benchmarks in `marshal_bench_test.go` measure cluster configuration
marshalling — the custom reflection-based logic that prunes default values and
serializes clusters to YAML/JSON (used when saving `ksail.yaml`, displaying
cluster information, and serializing cluster state).

Regression gating happens in CI via `github-action-benchmark` against its own
stored history (see `docs/BENCHMARK-REGRESSION.md`); no baseline files are
kept in-tree.

## What the Benchmarks Cover

| Benchmark                       | What it measures                                                                                                                                            |
|---------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `BenchmarkCluster_MarshalYAML`  | Custom `MarshalYAML()` across minimal, basic, CNI, GitOps, and full production configurations (default pruning + reflection-based struct-to-map conversion) |
| `BenchmarkCluster_MarshalJSON`  | Custom `MarshalJSON()` (used internally by the YAML encoder) across minimal, basic, and full configurations                                                 |
| `BenchmarkYAMLEncode`           | Full end-to-end `yaml.Marshal()` pipeline including YAML library overhead                                                                                   |
| `BenchmarkJSONEncode`           | Standard `json.Marshal()` performance as a comparison baseline                                                                                              |
| `BenchmarkPruneClusterDefaults` | Default pruning in isolation across mostly-default, mixed, and all-custom configurations                                                                    |

## Running the Benchmarks

```bash
# All marshalling benchmarks
go test -run=^$ -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/...

# A specific suite
go test -run=^$ -bench=BenchmarkCluster_MarshalYAML -benchmem ./pkg/apis/cluster/v1alpha1/...

# Longer runs for more stable results
go test -run=^$ -bench=. -benchmem -benchtime=5s ./pkg/apis/cluster/v1alpha1/...
```

To compare two local runs, save each to a file and use
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```bash
go test -run=^$ -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/... > old.txt
# ...make changes...
go test -run=^$ -bench=. -benchmem ./pkg/apis/cluster/v1alpha1/... > new.txt
benchstat old.txt new.txt
```
