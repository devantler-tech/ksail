# Image Extractor Benchmarks

This document describes the benchmark suite for KSail's image extractor and normalization utilities, and provides guidance for collecting baseline performance metrics.

## Overview

The image extractor benchmarks measure performance of:

1. **`ExtractImagesFromManifest`** — regex-based extraction of container image references from rendered Kubernetes YAML manifests
2. **`ExtractImagesFromMultipleManifests`** — multi-manifest deduplication across concatenated manifest sets
3. **`NormalizeImageRef`** — fully-qualified image reference normalization (adds registry, namespace, and `:latest` tag as needed)

The `imagePattern` regex is pre-compiled at package level, eliminating ~7500 ns and 93 allocations that were previously incurred on every call from repeated `regexp.MustCompile` invocations. Extraction performance is now dominated by I/O scanning and normalization rather than regex compilation.

## Running Benchmarks

```bash
# Run all image benchmarks
go test -bench=. -benchmem ./pkg/svc/image/...

# Run a specific benchmark group
go test -bench=BenchmarkExtractImagesFromManifest -benchmem ./pkg/svc/image/...
go test -bench=BenchmarkNormalizeImageRef -benchmem ./pkg/svc/image/...

# Save results for comparison
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/svc/image/... > baseline.txt

# Compare before/after changes
go test -bench=. -benchmem -count=5 -run=^$ ./pkg/svc/image/... > new.txt
benchstat baseline.txt new.txt
```

## Benchmark Scenarios

### Single Manifest Extraction

- **Small/3images**: Minimal Pod manifest with 1 init container and 2 containers — 3 unique images, ~20 YAML lines.
- **Medium/5images**: Deployment + Service + ConfigMap bundle with 2 init containers and 3 containers — 5 unique images across multiple documents.
- **Large/40images**: 20 repetitions of a DaemonSet manifest, each with 1 initContainer and 2 containers (3 image lines per repetition) — 60 image occurrences deduplicated to 3 unique images; exercises the deduplication map and scanner buffer scaling.

### Multi-Manifest Extraction

- **TwoManifests**: Combines the small (3 images) and medium (5 images) manifests — 7 unique images with deduplication across manifest boundaries.
- **TenManifests**: Ten copies of the small manifest — 3 unique images, exercises the cross-manifest `seen` map on repeated identical input.

### Image Reference Normalization

- **Simple**: Bare image name (`nginx`) → `docker.io/library/nginx:latest`
- **WithTag**: `nginx:1.25` → `docker.io/library/nginx:1.25`
- **DockerHubNamespaced**: `bitnami/nginx:1.25` → `docker.io/bitnami/nginx:1.25`
- **GHCR**: Full `ghcr.io/` reference — already has registry; only ensures tag.
- **RegistryK8s**: `registry.k8s.io/` reference with multi-level path.
- **Localhost**: `localhost:5000/myimage:v1` — port-based registry detection.
- **Digest**: `nginx@sha256:...` — digest references bypass tag insertion.

## Performance Notes

- The pre-compiled `imagePattern` package-level var eliminated ~7500 ns/op and 93 allocs/op of per-call `regexp.MustCompile` overhead. The remaining cost is scanner I/O, line-by-line regex matching, and string normalization.
- For the Large/40images scenario, most image references are duplicates — the `seen` map prevents repeated slice appends / retained results for duplicates, keeping allocations low relative to document size even though normalization still runs for each occurrence.
- `NormalizeImageRef` for registry-qualified images (GHCR, RegistryK8s) is faster than bare names because the registry-detection fast path skips namespace prefixing.
- The scanner max token size is increased to 1 MiB to handle long lines in Helm-rendered CRDs (e.g., Calico/Tigera); this higher limit is shared across all lines in a single `ExtractImagesFromManifest` call.
