# Containerd Image Caching Flow

## Before Caching (Rate Limiting Issues)

```
┌──────────────────────────────────────────────────────────────┐
│  GitHub Actions Runner                                        │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐    │
│  │  System Test                                          │    │
│  │                                                        │    │
│  │  1. Create Cluster (Kind/K3d/Talos)                  │    │
│  │     └─> Docker Container (cluster node)              │    │
│  │                                                        │    │
│  │  2. Deploy Applications                               │    │
│  │     ├─> cert-manager                                  │    │
│  │     ├─> flux                                          │    │
│  │     ├─> argocd                                        │    │
│  │     ├─> cilium/calico                                 │    │
│  │     └─> kyverno/gatekeeper                            │    │
│  │                                                        │    │
│  │  For EACH deployment:                                 │    │
│  │     containerd pulls images from registries ──┐       │    │
│  │                                                │       │    │
│  └────────────────────────────────────────────────┼───────┘    │
│                                                   │            │
└───────────────────────────────────────────────────┼────────────┘
                                                    │
                                                    ▼
                                    ┌───────────────────────────┐
                                    │  Public Registries        │
                                    │  - Docker Hub             │
                                    │  - GHCR                   │
                                    │  - Quay.io                │
                                    │  - registry.k8s.io        │
                                    │                           │
                                    │  ❌ Rate Limiting (429)   │
                                    │  ❌ Slow pulls            │
                                    │  ❌ Network costs         │
                                    └───────────────────────────┘
```

## After Caching (No Rate Limiting)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  GitHub Actions Runner                                                        │
│                                                                                │
│  ┌─────────────────────────────────────────────────────────────────┐         │
│  │  System Test with Caching                                        │         │
│  │                                                                   │         │
│  │  1. Create Cluster (Kind/K3d/Talos)                             │         │
│  │     └─> Docker Container (cluster node)                         │         │
│  │                                                                   │         │
│  │  2. Restore Cache ────────────┐                                 │         │
│  │     │                          │                                 │         │
│  │     ▼                          │                                 │         │
│  │  ┌─────────────────────┐      │                                 │         │
│  │  │  Cache Hit?         │      │                                 │         │
│  │  │  ✅ Yes → Load      │◄─────┘                                 │         │
│  │  │  ❌ No → Skip       │                                         │         │
│  │  └─────────────────────┘                                         │         │
│  │     │                                                             │         │
│  │     ▼                                                             │         │
│  │  Import images into containerd                                   │         │
│  │     (using ctr CLI)                                              │         │
│  │                                                                   │         │
│  │  3. Deploy Applications                                          │         │
│  │     ├─> cert-manager      ✅ Uses cached image                   │         │
│  │     ├─> flux              ✅ Uses cached image                   │         │
│  │     ├─> argocd            ✅ Uses cached image                   │         │
│  │     ├─> cilium/calico     ✅ Uses cached image                   │         │
│  │     └─> kyverno/gatekeeper ✅ Uses cached image                  │         │
│  │                                                                   │         │
│  │  4. Save Cache                                                   │         │
│  │     ├─> Export images from containerd (ctr export)              │         │
│  │     ├─> Save to tar archive                                     │         │
│  │     └─> Upload to GitHub Actions cache ───────────┐             │         │
│  │                                                     │             │         │
│  └─────────────────────────────────────────────────────┼─────────────┘         │
│                                                        │                       │
│                                                        ▼                       │
│  ┌───────────────────────────────────────────────────────────────┐            │
│  │  GitHub Actions Cache                                          │            │
│  │  ┌─────────────────────────────────────────────────────────┐  │            │
│  │  │  containerd-images-vanilla-v1  (~3 GB)                  │  │            │
│  │  │  containerd-images-k3s-v1      (~2 GB)                  │  │            │
│  │  │  containerd-images-talos-v1    (~3 GB)                  │  │            │
│  │  └─────────────────────────────────────────────────────────┘  │            │
│  │                                                                 │            │
│  │  ✅ Persists for 7 days                                        │            │
│  │  ✅ Shared across all tests for same distribution             │            │
│  │  ✅ Max 10GB per repository                                    │            │
│  └───────────────────────────────────────────────────────────────┘            │
│                                                                                │
└────────────────────────────────────────────────────────────────────────────────┘

     Public registries only accessed on cache miss (first run)
                          │
                          ▼
              ┌───────────────────────────┐
              │  Public Registries        │
              │  - Docker Hub             │
              │  - GHCR                   │
              │  - Quay.io                │
              │  - registry.k8s.io        │
              │                           │
              │  ✅ Rarely accessed       │
              │  ✅ No rate limiting      │
              │  ✅ Reduced network load  │
              └───────────────────────────┘
```

## Cache Lifecycle

### First Run (Cache Miss)

```
┌───────────────┐
│ Start Test    │
└───────┬───────┘
        │
        ▼
┌─────────────────────┐
│ Create Cluster      │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Restore Cache       │
│ Result: MISS ❌     │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Deploy Apps         │
│ (pulls from         │
│  registries)        │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Save Cache          │
│ - Export images     │
│ - Create tar        │
│ - Upload cache      │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Delete Cluster      │
└─────────────────────┘

Time: Baseline + 30s (export overhead)
Network: 2-4 GB downloaded
```

### Subsequent Runs (Cache Hit)

```
┌───────────────┐
│ Start Test    │
└───────┬───────┘
        │
        ▼
┌─────────────────────┐
│ Create Cluster      │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Restore Cache       │
│ Result: HIT ✅      │
│ - Download tar      │
│ - Import to ctr     │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Deploy Apps         │
│ (uses cached        │
│  images)            │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Save Cache          │
│ (updates if needed) │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ Delete Cluster      │
└─────────────────────┘

Time: Baseline - 2-5 min (no image pulls)
Network: 0 bytes downloaded from registries
```

## Performance Impact

### Before Caching

- **Time**: Variable (depends on registry speed)
- **Network**: 2-4 GB per test × N tests = high bandwidth
- **Reliability**: ❌ Subject to rate limits
- **Cost**: High (registry API calls, bandwidth)

### After Caching

- **First Run**: +30s (one-time export cost)
- **Subsequent Runs**: -2 to -5 min (no image pulls)
- **Network**: ~0 GB from registries (cached)
- **Reliability**: ✅ Independent of registry availability
- **Cost**: Low (GitHub Actions cache is free)

## Key Benefits

1. **Rate Limiting Prevention**: Eliminates 429 errors
2. **Performance**: 2-5 min faster per test (after first run)
3. **Reliability**: Tests don't fail due to registry issues
4. **Cost Savings**: Reduced network egress and registry API calls
5. **Scalability**: Can run many parallel tests without hitting limits
