# Build & CI Performance Optimization Guide for KSail

This guide focuses on optimizing build times, test execution, CI/CD pipelines, and development workflow performance for the KSail project.

## Quick Performance Checks

### Build Time Measurement

```bash
# Clean build (no cache)
go clean -cache -modcache
time go build -o ksail .

# Cached build
touch main.go
time go build -o ksail .

# Parallel build (default is GOMAXPROCS)
time go build -p 4 -o ksail .
```

### Test Performance

```bash
# Measure test suite time
time go test ./...

# Identify slow tests
go test -v ./... | grep -E "PASS|FAIL" | grep -E "[0-9]+\.[0-9]+s"

# Run tests in parallel
go test -parallel 8 ./...

# Run specific package tests
time go test ./pkg/svc/provisioner/cluster/...
```

### CI Pipeline Analysis

```bash
# View workflow run times
gh run list --workflow=ci.yaml --limit 10

# View job timings
gh run view <run-id> --log

# Compare run times over time
gh run list --workflow=ci.yaml --limit 50 --json conclusion,createdAt,updatedAt > ci-history.json
```

## Build Optimization

### 1. Dependency Management

**Module Cache Optimization:**

```bash
# Use Go module cache in CI
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
    cache: true  # Automatically cache GOMODCACHE and build cache

# Pre-download dependencies
go mod download

# Verify dependencies are minimal
go mod tidy
go mod verify
```

**Reduce Dependency Bloat:**

```bash
# Analyze dependencies
go mod graph | grep -v "std" | head -20

# Find why a dependency is included
go mod why -m github.com/heavy/dependency

# Use lighter alternatives where possible
# Example: Consider k8s.io/client-go carefully (large dependency tree)
```

### 2. Build Cache Optimization

**Effective Caching Strategy:**

```yaml
# GitHub Actions cache configuration
- name: Cache Go modules
  uses: actions/cache@v4
  with:
    path: |
      ~/.cache/go-build
      ~/go/pkg/mod
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    restore-keys: |
      ${{ runner.os }}-go-
```

**Build Cache in Docker:**

```dockerfile
# Multi-stage build with cache mounts
FROM golang:1.25.4 AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o ksail .
```

### 3. Compilation Optimization

**Build Flags:**

```bash
# Faster compilation (less optimization)
go build -gcflags=-G=3 -o ksail .

# Parallel compilation
go build -p 8 -o ksail .

# Disable debug symbols for CI builds
go build -ldflags="-s -w" -o ksail .

# Production build with optimizations
go build -trimpath -ldflags="-s -w -X main.version=$(git describe --tags)" -o ksail .
```

## Test Optimization

### 1. Parallel Test Execution

**Package-Level Parallelism:**

```bash
# Run packages in parallel (default)
go test ./...

# Increase parallelism
go test -p 8 ./...

# Test-level parallelism
go test -parallel 16 ./...
```

**Smart Test Organization:**

```go
// Mark independent tests as parallel
func TestClusterCreate(t *testing.T) {
    t.Parallel()  // Run in parallel with other parallel tests
    // Test code...
}

// Don't parallelize tests that share resources
func TestDockerCleanup(t *testing.T) {
    // No t.Parallel() - modifies global Docker state
}
```

### 2. Test Caching

**Leverage Test Cache:**

```bash
# Go caches test results automatically
go test ./...  # Initial run
go test ./...  # Cached - instant if nothing changed

# Force re-run
go test -count=1 ./...

# Clear test cache
go clean -testcache
```

**Write Deterministic Tests:**

```go
// ❌ Non-deterministic - cache disabled
func TestRandom(t *testing.T) {
    if rand.Intn(100) > 50 {  // Different every run
        t.Fail()
    }
}

// ✅ Deterministic - cache enabled
func TestParsing(t *testing.T) {
    input := "test data"
    result := parse(input)
    assert.Equal(t, expected, result)
}
```

### 3. Fast vs. Slow Test Separation

**Build Tags for Slow Tests:**

```go
//go:build integration

package provisioner_test

// Integration tests that create real clusters
func TestRealClusterCreation(t *testing.T) {
    // Slow test...
}
```

```bash
# Fast unit tests only
go test ./...

# Include integration tests
go test -tags=integration ./...

# In CI: separate jobs for fast/slow tests
```

### 4. Skip Expensive Operations in Tests

**Use Mocks and Fakes:**

```go
// ❌ Slow: real Docker operations
func TestClusterInfo(t *testing.T) {
    docker := docker.NewClient()
    cluster.Create(docker)  // Slow!
    info := cluster.Info()
    cluster.Delete(docker)  // Slow!
}

// ✅ Fast: mocked Docker client
func TestClusterInfo(t *testing.T) {
    mockDocker := NewMockDockerClient()
    mockDocker.On("ContainerList").Return(testContainers)
    info := cluster.Info(mockDocker)
    assert.Equal(t, expected, info)
}
```

## CI/CD Optimization

### 1. Conditional Workflow Execution

**Path Filtering (Already Implemented):**

```yaml
# Only run tests when code changes
- uses: dorny/paths-filter@v3
  id: filter
  with:
    filters: |
      code:
        - '**/*.go'
        - 'go.mod'
        - 'go.sum'

# Conditional job execution
if: needs.changes.outputs.code == 'true'
```

### 2. Matrix Strategy Optimization

**Reduce Matrix Combinations:**

```yaml
# ❌ Too many combinations (slow, expensive)
matrix:
  distribution: [Vanilla, K3s, Talos]
  provider: [Docker, Hetzner]
  cni: [None, Cilium, Calico]
  # 3 × 2 × 3 = 18 jobs!

# ✅ Strategic combinations
matrix:
  include:
    - distribution: Vanilla
      provider: Docker
      cni: None
    - distribution: K3s
      provider: Docker
      cni: Cilium
    - distribution: Talos
      provider: Hetzner
      cni: Calico
  # Only 3 jobs - covers key scenarios
```

### 3. Artifact Caching

**Binary Caching (Already Implemented):**

```yaml
- name: Cache KSail Binary
  uses: ./.github/actions/cache-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
```

**Image Caching (Already Implemented):**

```yaml
- name: Cache Cluster Images
  uses: ./.github/actions/cache-cluster-images
  # Caches Docker images to speed up cluster creation
```

### 4. Concurrent Workflow Jobs

**Maximize Parallelism:**

```yaml
jobs:
  # These can run in parallel
  lint:
    runs-on: ubuntu-latest
  
  test-unit:
    runs-on: ubuntu-latest
  
  build:
    runs-on: ubuntu-latest
  
  # This depends on build
  test-system:
    needs: [build]
    runs-on: ubuntu-latest
```

### 5. Runner Optimization

**Choose Appropriate Runners:**

```yaml
# Standard 2-core runner for most jobs
runs-on: ubuntu-latest

# Larger runner for heavy builds/tests
runs-on: ubuntu-latest-8-cores  # If available

# Self-hosted runners with cache persistence
runs-on: [self-hosted, linux, x64]
```

## Development Workflow Performance

### 1. Local Development Setup

**Fast Iteration Loop:**

```bash
# Use air for auto-reload during development
go install github.com/cosmtrek/air@latest
air  # Watches files and rebuilds on changes

# Or use go run for quick tests
go run . cluster info

# Pre-commit hooks for fast feedback
# .git/hooks/pre-commit
go fmt ./...
golangci-lint run --fast
```

### 2. Incremental Builds

**Optimize for Quick Rebuilds:**

```bash
# Only rebuild changed packages
go install ./cmd/ksail  # Faster than go build

# Use build cache effectively
export GOCACHE=$(go env GOCACHE)  # Persist cache location

# Monitor cache hit rate
go clean -cache
time go build .  # First build
time touch pkg/cli/cmd/cluster.go
time go build .  # Incremental build
```

### 3. Documentation Build Optimization

**Astro Build Performance:**

```bash
# Development mode (fast, no optimization)
cd docs
npm run dev

# Production build (optimized)
npm run build

# Clear cache for clean build
rm -rf node_modules/.astro

# Parallel build (if supported)
NODE_OPTIONS="--max-old-space-size=4096" npm run build
```

## Performance Monitoring

### 1. Track Build Times Over Time

**CI Time Series:**

```bash
# Export CI run times
gh api repos/devantler-tech/ksail/actions/workflows/ci.yaml/runs \
  --paginate \
  --jq '.workflow_runs[] | {created: .created_at, duration: .run_duration_ms}' \
  > ci-times.json

# Plot with gnuplot or similar
```

### 2. Test Performance Regression Detection

**Automated Checks:**

```yaml
# Add to CI
- name: Run benchmarks
  run: go test -bench=. -benchmem ./... > new-benchmarks.txt

- name: Compare with baseline
  run: |
    # Download baseline from previous run
    benchstat baseline.txt new-benchmarks.txt
    # Fail if performance regression > 20%
```

### 3. Profile Build Process

**Detailed Build Analysis:**

```bash
# Trace build execution
go build -x -o ksail . 2>&1 | tee build-trace.log

# Analyze what's taking time
grep "compile" build-trace.log | wc -l
grep "link" build-trace.log
```

## Optimization Checklist

- [ ] Go module cache enabled in CI
- [ ] Build cache configured effectively
- [ ] Tests marked as parallel where safe
- [ ] Integration tests separated with build tags
- [ ] CI matrix minimized to essential combinations
- [ ] Path filters prevent unnecessary runs
- [ ] Artifacts cached between jobs
- [ ] Benchmark baseline established
- [ ] Build times tracked over time
- [ ] Documentation builds optimized

## Success Metrics

**Build Performance Targets:**

- Go build (cold cache): <2m
- Go build (warm cache): <30s
- Unit test suite: <30s
- Linting (golangci-lint): <1m
- Documentation build: <10s

**CI Performance Targets:**

- Unit test job: <2m
- System test job: <5m per matrix entry
- Total CI time (PR): <15m
- Cache hit rate: >80% for binary, >60% for images

**Developer Experience:**

- Local build iteration: <5s
- Pre-commit hooks: <10s
- Test feedback: <30s for relevant tests
