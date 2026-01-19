# CLI Performance & User Experience Guide for KSail

This guide focuses on optimizing the end-user experience of the KSail CLI, covering startup time, operation speed, progress feedback, and efficient resource usage.

## Quick Performance Testing

### CLI Startup Time
```bash
# Measure startup time
time ./ksail --version
time ./ksail cluster info

# Profile startup
go build -o ksail
time ./ksail --help

# Target: <100ms for simple commands
```

### Operation Timing
```bash
# Measure cluster creation
time ./ksail cluster create

# Measure workload apply
time ./ksail workload apply

# Compare distributions
for dist in Vanilla K3s Talos; do
  echo "=== $dist ==="
  time ./ksail cluster create --distribution $dist
done
```

### Resource Usage Monitoring
```bash
# Monitor CPU/memory during operations
/usr/bin/time -v ./ksail cluster create

# Watch Docker resource usage
docker stats
```

## CLI Performance Optimization Areas

### 1. Startup Time Optimization

**Lazy Initialization:**
```go
// ❌ Initialize everything at startup
func init() {
    dockerClient = docker.NewClient()
    helmClient = helm.NewClient()
    kubectlClient = kubectl.NewClient()
}

// ✅ Initialize only when needed
var (
    dockerClient  *docker.Client
    dockerOnce    sync.Once
)

func getDockerClient() *docker.Client {
    dockerOnce.Do(func() {
        dockerClient = docker.NewClient()
    })
    return dockerClient
}
```

**Command Organization:**
```go
// Keep cobra command initialization lightweight
// Defer heavy operations to Run functions
var clusterCreateCmd = &cobra.Command{
    Use:   "create",
    Short: "Create a Kubernetes cluster",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Heavy initialization here, not in init()
        return runClusterCreate(cmd.Context())
    },
}
```

### 2. Progress Indication

**Essential for Long Operations:**
```go
import "github.com/schollz/progressbar/v3"

func createCluster(ctx context.Context) error {
    steps := []string{
        "Pulling container images",
        "Creating cluster nodes",
        "Installing CNI",
        "Installing cert-manager",
        "Applying workloads",
    }
    
    bar := progressbar.NewOptions(len(steps),
        progressbar.OptionSetDescription("Creating cluster"),
        progressbar.OptionShowCount(),
        progressbar.OptionSetWidth(40),
    )
    
    for _, step := range steps {
        bar.Describe(step)
        // Execute step...
        bar.Add(1)
    }
    
    return nil
}
```

**Spinner for Indeterminate Operations:**
```go
import "github.com/briandowns/spinner"

func pullImage(image string) error {
    s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
    s.Suffix = fmt.Sprintf(" Pulling %s...", image)
    s.Start()
    defer s.Stop()
    
    // Pull image...
    return nil
}
```

### 3. Parallel Operations

**Concurrent Image Pulling:**
```go
func pullImages(images []string) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(images))
    
    // Limit concurrent pulls
    sem := make(chan struct{}, 3)
    
    for _, img := range images {
        wg.Add(1)
        go func(image string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()
            
            if err := pullImage(image); err != nil {
                errChan <- err
            }
        }(img)
    }
    
    wg.Wait()
    close(errChan)
    
    var errs []error
    for err := range errChan {
        errs = append(errs, err)
    }
    return errors.Join(errs...)
}
```

**Parallel Workload Application:**
```go
// Apply independent workloads concurrently
func applyWorkloads(workloads []Workload) error {
    // Group by dependencies
    independent, dependent := groupByDependencies(workloads)
    
    // Apply independent ones in parallel
    if err := applyParallel(independent); err != nil {
        return err
    }
    
    // Apply dependent ones sequentially
    return applySequential(dependent)
}
```

### 4. Caching Strategies

**Cache Expensive Operations:**
```go
type ClusterInfoCache struct {
    mu    sync.RWMutex
    cache map[string]*ClusterInfo
    ttl   time.Duration
}

func (c *ClusterInfoCache) Get(name string) (*ClusterInfo, error) {
    c.mu.RLock()
    if info, ok := c.cache[name]; ok {
        if time.Since(info.FetchedAt) < c.ttl {
            c.mu.RUnlock()
            return info, nil
        }
    }
    c.mu.RUnlock()
    
    // Cache miss or expired - fetch fresh data
    info, err := fetchClusterInfo(name)
    if err != nil {
        return nil, err
    }
    
    c.mu.Lock()
    c.cache[name] = info
    c.mu.Unlock()
    
    return info, nil
}
```

**Cache Docker Images:**
- Use `.github/actions/cache-cluster-images` pattern
- Pre-pull common images
- Share image cache across clusters

### 5. Efficient API Usage

**Docker API Optimization:**
```go
// ❌ Multiple calls for same data
func getClusterInfo(name string) (*Info, error) {
    containers, _ := docker.ContainerList(ctx, opts)
    for _, c := range containers {
        inspect, _ := docker.ContainerInspect(ctx, c.ID)
        // Use inspect data...
    }
}

// ✅ Single call with all needed data
func getClusterInfo(name string) (*Info, error) {
    containers, _ := docker.ContainerList(ctx, types.ContainerListOptions{
        Filters: filters.NewArgs(
            filters.Arg("label", fmt.Sprintf("ksail.cluster=%s", name)),
        ),
        All: true,
    })
    // All data available in first call
}
```

**Kubernetes API Optimization:**
```go
// Use cached discovery client
discoveryClient := cached.NewMemCacheClient(discovery.NewDiscoveryClient(restConfig))

// Batch operations
patch := []byte(`{"metadata":{"labels":{"app":"ksail"}}}`)
for _, pod := range pods {
    // Apply in goroutines with rate limiting
    go applyPatch(pod, patch)
}
```

## User Experience Enhancements

### 1. Informative Output

**Show Timing Information:**
```go
func runClusterCreate(ctx context.Context) error {
    start := time.Now()
    defer func() {
        fmt.Printf("\n✅ Cluster created in %v\n", time.Since(start).Round(time.Second))
    }()
    
    // Create cluster...
    return nil
}
```

**Helpful Error Messages:**
```go
// ❌ Cryptic error
return fmt.Errorf("failed to create cluster")

// ✅ Actionable error
return fmt.Errorf("failed to create cluster: Docker daemon not running. Start Docker and try again")
```

### 2. Smart Defaults

**Optimize Default Configurations:**
```go
// Choose sensible defaults based on system resources
func defaultWorkerNodes() int {
    cpus := runtime.NumCPU()
    switch {
    case cpus <= 4:
        return 1
    case cpus <= 8:
        return 2
    default:
        return 3
    }
}
```

### 3. Cancellation Support

**Respect Context Cancellation:**
```go
func createCluster(ctx context.Context) error {
    steps := []func(context.Context) error{
        pullImages,
        createNodes,
        installCNI,
    }
    
    for _, step := range steps {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if err := step(ctx); err != nil {
                return err
            }
        }
    }
    return nil
}
```

## Performance Testing Scenarios

### 1. Cold Start (First Run)
```bash
# Clear all caches
docker system prune -af
rm -rf ~/.kube ~/.ksail

# Measure first cluster creation
time ./ksail cluster init
time ./ksail cluster create
```

### 2. Warm Start (Cached Images)
```bash
# Measure with cached images
time ./ksail cluster delete
time ./ksail cluster create
```

### 3. Multi-Cluster Operations
```bash
# Test cluster list performance
for i in {1..10}; do
  ./ksail cluster create --name cluster-$i &
done
wait

time ./ksail cluster list
```

### 4. Large Workload Application
```bash
# Generate large kustomization
for i in {1..100}; do
  echo "---" >> k8s/large-app.yaml
  echo "apiVersion: v1" >> k8s/large-app.yaml
  echo "kind: ConfigMap" >> k8s/large-app.yaml
  echo "metadata:" >> k8s/large-app.yaml
  echo "  name: config-$i" >> k8s/large-app.yaml
done

time ./ksail workload apply
```

## Performance Benchmarks

**Create Baseline:**
```bash
# Measure key operations
{
  echo "=== CLI Startup ==="
  time ./ksail --version
  
  echo "=== Cluster Create (Vanilla) ==="
  time ./ksail cluster create --distribution Vanilla
  
  echo "=== Cluster Create (K3s) ==="
  time ./ksail cluster delete && time ./ksail cluster create --distribution K3s
  
  echo "=== Workload Apply ==="
  time ./ksail workload apply
  
  echo "=== Cluster Info ==="
  time ./ksail cluster info
} > performance-baseline.txt
```

## Success Metrics

**CLI Performance Targets:**
- Startup time (--version): <100ms
- Cluster info: <500ms
- Cluster create (Vanilla, cached): <60s
- Cluster create (K3s, cached): <45s
- Workload apply (10 resources): <10s

**User Experience Indicators:**
- Progress shown for operations >5s
- Estimated time remaining for operations >30s
- Cancellable with Ctrl+C
- Clear error messages with remediation steps
- No silent failures or hangs

## Optimization Checklist

- [ ] Startup time profiled and optimized
- [ ] Long operations show progress indicators
- [ ] Parallel operations where safe
- [ ] Caching implemented for expensive calls
- [ ] Context cancellation respected
- [ ] Error messages are actionable
- [ ] Resource usage monitored
- [ ] Benchmarks show improvement
- [ ] User experience tested manually
- [ ] Documentation updated
