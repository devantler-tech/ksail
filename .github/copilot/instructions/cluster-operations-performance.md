# Cluster Operations Performance Guide for KSail

This guide covers performance optimization for cluster provisioning, workload management, and provider-specific operations in KSail.

## Quick Performance Testing

### Cluster Creation Benchmarking

```bash
# Benchmark different distributions
for dist in Vanilla K3s Talos; do
  echo "=== Testing $dist ==="
  ./ksail cluster delete -f
  /usr/bin/time -v ./ksail cluster create --distribution $dist 2>&1 | tee ${dist}-perf.log
done

# Extract timing
grep "Elapsed" *-perf.log
```

### Provider Performance Comparison

```bash
# Docker provider (local)
time ./ksail cluster create --provider Docker --distribution Talos

# Hetzner provider (cloud)
time ./ksail cluster create --provider Hetzner --distribution Talos
```

### Workload Application Performance

```bash
# Single workload
time ./ksail workload apply

# Multiple workloads
time ./ksail workload apply --overlay-path overlay1 --overlay-path overlay2

# Large kustomization
time kubectl apply -k ./k8s
```

## Cluster Provisioning Optimization

### 1. Image Pre-pulling

**Container Image Caching:**

```go
// Pre-pull images in parallel before cluster creation
func prepullImages(images []string) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(images))
    sem := make(chan struct{}, 3)  // Limit concurrent pulls
    
    for _, img := range images {
        wg.Add(1)
        go func(image string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()
            
            if err := docker.ImagePull(ctx, image); err != nil {
                errChan <- fmt.Errorf("failed to pull %s: %w", image, err)
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

**Smart Image Selection:**

```go
// Use specific image versions to enable caching
const (
    KindNodeImage   = "kindest/node:v1.31.0"
    K3sImage        = "rancher/k3s:v1.31.0-k3s1"
    TalosImage      = "ghcr.io/siderolabs/talos:v1.13.0"
)

// Avoid "latest" tags - they defeat caching
```

### 2. Parallel Node Creation

**Concurrent Node Provisioning:**

```go
// Create control plane and workers in parallel
func createClusterNodes(cfg *ClusterConfig) error {
    var wg sync.WaitGroup
    errChan := make(chan error, cfg.NodeCount)
    
    // Create control plane node(s)
    for i := 0; i < cfg.ControlPlaneNodes; i++ {
        wg.Add(1)
        go func(index int) {
            defer wg.Done()
            if err := createNode(cfg, "control-plane", index); err != nil {
                errChan <- err
            }
        }(i)
    }
    
    // Create worker nodes
    for i := 0; i < cfg.WorkerNodes; i++ {
        wg.Add(1)
        go func(index int) {
            defer wg.Done()
            if err := createNode(cfg, "worker", index); err != nil {
                errChan <- err
            }
        }(i)
    }
    
    wg.Wait()
    close(errChan)
    
    return collectErrors(errChan)
}
```

### 3. Optimized Cluster Initialization

**Fast CNI Installation:**

```go
// Install CNI before cluster is fully ready
func installCNI(cluster *Cluster) error {
    // Don't wait for all nodes - install as soon as control plane is ready
    if err := waitForControlPlane(cluster); err != nil {
        return err
    }
    
    // Install CNI immediately
    return applyCNI(cluster)
}

// Overlap operations where possible
func createCluster(cfg *ClusterConfig) error {
    // Start nodes
    nodesCh := make(chan error, 1)
    go func() {
        nodesCh <- createClusterNodes(cfg)
    }()
    
    // Meanwhile, prepare manifests
    manifests, err := prepareManifests(cfg)
    if err != nil {
        return err
    }
    
    // Wait for nodes
    if err := <-nodesCh; err != nil {
        return err
    }
    
    // Apply manifests
    return applyManifests(manifests)
}
```

### 4. State Detection Optimization

**Efficient Cluster State Checks:**

```go
// ❌ Slow: Multiple separate API calls
func isClusterReady(name string) bool {
    containers, _ := docker.ContainerList(ctx, opts)
    for _, c := range containers {
        inspect, _ := docker.ContainerInspect(ctx, c.ID)
        if !inspect.State.Running {
            return false
        }
    }
    return true
}

// ✅ Fast: Single API call with filters
func isClusterReady(name string) bool {
    containers, _ := docker.ContainerList(ctx, types.ContainerListOptions{
        Filters: filters.NewArgs(
            filters.Arg("label", fmt.Sprintf("ksail.cluster=%s", name)),
            filters.Arg("health", "healthy"),
        ),
    })
    return len(containers) > 0
}
```

**Polling Optimization:**

```go
// Use exponential backoff for polling
func waitForClusterReady(cluster *Cluster, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    backoff := 100 * time.Millisecond
    maxBackoff := 5 * time.Second
    
    for {
        if ready, err := cluster.IsReady(ctx); err == nil && ready {
            return nil
        }
        
        select {
        case <-ctx.Done():
            return fmt.Errorf("timeout waiting for cluster")
        case <-time.After(backoff):
            backoff *= 2
            if backoff > maxBackoff {
                backoff = maxBackoff
            }
        }
    }
}
```

## Provider-Specific Optimizations

### 1. Docker Provider

**Connection Pooling:**

```go
// Reuse Docker client connections
var (
    dockerClient     *client.Client
    dockerClientOnce sync.Once
)

func getDockerClient() (*client.Client, error) {
    var err error
    dockerClientOnce.Do(func() {
        dockerClient, err = client.NewClientWithOpts(
            client.FromEnv,
            client.WithAPIVersionNegotiation(),
        )
    })
    return dockerClient, err
}
```

**Batch Operations:**

```go
// Delete multiple containers in parallel
func deleteCluster(name string) error {
    containers, _ := listClusterContainers(name)
    
    var wg sync.WaitGroup
    for _, c := range containers {
        wg.Add(1)
        go func(id string) {
            defer wg.Done()
            docker.ContainerRemove(ctx, id, types.ContainerRemoveOptions{
                Force: true,
            })
        }(c.ID)
    }
    wg.Wait()
    
    return nil
}
```

### 2. Hetzner Provider

**API Call Minimization:**

```go
// ❌ Multiple API calls
func getServerInfo(client *hcloud.Client, serverID int64) (*ServerInfo, error) {
    server, _, _ := client.Server.GetByID(ctx, serverID)
    ip, _, _ := client.FloatingIP.GetByID(ctx, server.PrimaryIP.ID)
    network, _, _ := client.Network.GetByID(ctx, server.PrivateNet[0].Network.ID)
    // Multiple round trips to API
}

// ✅ Batch with ListOpts
func getAllServerInfo(client *hcloud.Client) ([]*hcloud.Server, error) {
    servers, err := client.Server.All(ctx)
    // Single API call returns all data
    return servers, err
}
```

**Parallel Resource Creation:**

```go
func createHetznerCluster(cfg *ClusterConfig) error {
    var wg sync.WaitGroup
    resources := []func() error{
        func() error { return createNetwork(cfg) },
        func() error { return createFirewall(cfg) },
        func() error { return createSSHKey(cfg) },
    }
    
    errChan := make(chan error, len(resources))
    for _, fn := range resources {
        wg.Add(1)
        go func(f func() error) {
            defer wg.Done()
            if err := f(); err != nil {
                errChan <- err
            }
        }(fn)
    }
    
    wg.Wait()
    close(errChan)
    
    if err := collectErrors(errChan); err != nil {
        return err
    }
    
    // Create servers after prerequisites
    return createServers(cfg)
}
```

**Cleanup Efficiency:**

```go
// Batch cleanup to minimize API calls
func cleanupHetznerResources(client *hcloud.Client, prefix string) error {
    // Get all resources in parallel
    var wg sync.WaitGroup
    var servers []*hcloud.Server
    var volumes []*hcloud.Volume
    var networks []*hcloud.Network
    
    wg.Add(3)
    go func() {
        defer wg.Done()
        servers, _ = client.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
            ListOpts: hcloud.ListOpts{
                LabelSelector: fmt.Sprintf("ksail.cluster=%s", prefix),
            },
        })
    }()
    go func() {
        defer wg.Done()
        volumes, _ = client.Volume.AllWithOpts(ctx, hcloud.VolumeListOpts{
            ListOpts: hcloud.ListOpts{
                LabelSelector: fmt.Sprintf("ksail.cluster=%s", prefix),
            },
        })
    }()
    go func() {
        defer wg.Done()
        networks, _ = client.Network.AllWithOpts(ctx, hcloud.NetworkListOpts{
            ListOpts: hcloud.ListOpts{
                LabelSelector: fmt.Sprintf("ksail.cluster=%s", prefix),
            },
        })
    }()
    wg.Wait()
    
    // Delete in parallel
    return deleteResources(servers, volumes, networks)
}
```

## Workload Management Optimization

### 1. Manifest Processing

**Efficient YAML Parsing:**

```go
// Use streaming parser for large files
func parseManifests(reader io.Reader) ([]Manifest, error) {
    decoder := yaml.NewDecoder(reader)
    decoder.SetStrict(false)  // More lenient parsing
    
    var manifests []Manifest
    for {
        var m Manifest
        if err := decoder.Decode(&m); err == io.EOF {
            break
        } else if err != nil {
            return nil, err
        }
        manifests = append(manifests, m)
    }
    return manifests, nil
}
```

**Parallel Application:**

```go
// Apply independent resources in parallel
func applyManifests(manifests []Manifest) error {
    // Group by dependency order
    groups := groupByDependency(manifests)
    
    // Apply each group in sequence, items within group in parallel
    for _, group := range groups {
        if err := applyGroup(group); err != nil {
            return err
        }
    }
    return nil
}

func applyGroup(manifests []Manifest) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(manifests))
    
    for _, m := range manifests {
        wg.Add(1)
        go func(manifest Manifest) {
            defer wg.Done()
            if err := kubectl.Apply(manifest); err != nil {
                errChan <- err
            }
        }(m)
    }
    
    wg.Wait()
    close(errChan)
    return collectErrors(errChan)
}
```

### 2. GitOps Optimization

**Flux Reconciliation:**

```go
// Trigger reconciliation without waiting
func triggerFluxReconcile(namespace, name string) error {
    // Use kubectl annotate instead of waiting for interval
    cmd := exec.Command("kubectl", "annotate", "kustomization", name,
        "-n", namespace,
        "reconcile.fluxcd.io/requestedAt="+time.Now().Format(time.RFC3339),
        "--overwrite")
    return cmd.Run()
}
```

**ArgoCD Sync:**

```go
// Parallel app sync
func syncArgoApps(apps []string) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(apps))
    
    for _, app := range apps {
        wg.Add(1)
        go func(appName string) {
            defer wg.Done()
            if err := argocd.AppSync(appName); err != nil {
                errChan <- err
            }
        }(app)
    }
    
    wg.Wait()
    close(errChan)
    return collectErrors(errChan)
}
```

## Performance Monitoring

### Cluster Operation Metrics

```bash
# Track operation times
export KSAIL_TIMING_LOG=timing.log

# Extract cluster creation times
grep "cluster create" timing.log | awk '{print $NF}'

# Analyze by distribution
grep "Vanilla" timing.log | awk '{sum+=$NF; count++} END {print sum/count}'
```

### Resource Usage Tracking

```bash
# Monitor during cluster creation
docker stats --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" &
STATS_PID=$!

./ksail cluster create

kill $STATS_PID
```

## Success Metrics

**Cluster Creation Targets:**

- Vanilla (Docker, cached images): <60s
- K3s (Docker, cached images): <45s
- Talos (Docker, cached images): <90s
- Talos (Hetzner, cold): <3m

**Workload Application:**

- 10 resources: <10s
- 100 resources: <30s
- GitOps reconciliation: <1m

**State Detection:**

- Cluster ready check: <100ms
- Node health check: <500ms

## Optimization Checklist

- [ ] Images pre-pulled in parallel
- [ ] Nodes created concurrently
- [ ] CNI installed early in process
- [ ] State checks use efficient filters
- [ ] Exponential backoff for polling
- [ ] Docker client connection reused
- [ ] Hetzner API calls minimized
- [ ] Manifests applied in parallel where safe
- [ ] Timing metrics collected
- [ ] Resource usage monitored
