# Cache Containerd Images

This composite action caches containerd images from inside Kubernetes clusters to avoid rate limiting errors when pulling deployment images during CI workflows.

## Purpose

The system tests in the CI workflow deploy various applications (cert-manager, flux, argocd, cilium, calico, etc.) into Kubernetes clusters. These deployments pull images from container registries using **containerd inside the cluster containers** (Kind, K3d, or Talos nodes).

Without caching, each workflow run pulls these deployment images fresh from registries, which can lead to:

- **Rate limiting errors (429 responses)** from Docker Hub, GHCR, and other registries
- Slower workflow execution times
- Unreliable CI runs

This action solves these issues by:

1. Exporting containerd images from the cluster after deployments are applied
2. Caching the exported images using GitHub Actions cache
3. Restoring and importing images into containerd on subsequent runs

## How It Works

### Restore Operation

1. Attempts to restore cached containerd images from previous workflow runs
2. If cache hit, copies the image archive into the cluster container
3. Uses `ctr` (containerd CLI) to import images into containerd's image store
4. Subsequent deployments use the cached images instead of pulling from registries

### Save Operation

1. After deployments complete, lists all images in containerd
2. Uses `ctr` to export all images to a tar archive
3. Copies the archive from the cluster container to the host
4. Saves the archive to GitHub Actions cache for future runs

## Inputs

### `cluster-name` (required)

Name of the cluster to cache images from/to.

### `distribution` (required)

Kubernetes distribution type. Determines the container naming pattern:

- **Vanilla**: Kind containers (pattern: `<cluster-name>-control-plane`)
- **K3s**: K3d containers (pattern: `k3d-<cluster-name>-server-0`)
- **Talos**: Talos containers (pattern: `talos-<cluster-name>-controlplane-1`)

### `provider` (required)

Infrastructure provider. Currently only `Docker` is supported (Hetzner clusters don't use local containers).

- Default: `"Docker"`

### `operation` (required)

Operation to perform:

- **`restore`**: Restore and import cached images before deployments
- **`save`**: Export and save images after deployments

## Outputs

### `cache-hit`

Whether the cache was hit during restore operation (`'true'` or `'false'`).

## Usage

### Restore Images Before Deployments

```yaml
- name: 📦 Restore Containerd Cache
  uses: ./.github/actions/cache-docker-images
  with:
    cluster-name: my-cluster
    distribution: Vanilla
    provider: Docker
    operation: restore
```

### Save Images After Deployments

```yaml
- name: 💾 Save Containerd Cache
  uses: ./.github/actions/cache-docker-images
  with:
    cluster-name: my-cluster
    distribution: Vanilla
    provider: Docker
    operation: save
```

## Complete Example

```yaml
jobs:
  system-test:
    runs-on: ubuntu-latest
    steps:
      - name: 📄 Checkout
        uses: actions/checkout@v6
      
      - name: 📦 Restore Containerd Cache
        uses: ./.github/actions/cache-docker-images
        with:
          cluster-name: test-cluster
          distribution: ${{ matrix.distribution }}
          provider: Docker
          operation: restore
      
      - name: 🧪 Create cluster
        run: ksail cluster create --name test-cluster
      
      - name: 🚀 Deploy applications
        run: |
          ksail workload apply -k manifests/
      
      - name: 💾 Save Containerd Cache
        if: always()
        uses: ./.github/actions/cache-docker-images
        with:
          cluster-name: test-cluster
          distribution: ${{ matrix.distribution }}
          provider: Docker
          operation: save
      
      - name: 🧹 Delete cluster
        if: always()
        run: ksail cluster delete
```

## Cache Key Strategy

The cache key is based on the distribution type:

- `containerd-images-vanilla-v1` for Kind clusters
- `containerd-images-k3s-v1` for K3d clusters
- `containerd-images-talos-v1` for Talos clusters

This means all test runs for the same distribution share the same cache, maximizing reuse across different test configurations.

## Cache Invalidation

To invalidate the cache and force fresh image pulls:

1. Increment the version suffix in the cache key (e.g., `v1` → `v2`)
2. Edit the action's cache key generation logic

## Limitations

- Only works with Docker provider (not Hetzner cloud clusters)
- Cache size is limited by GitHub Actions cache limits (10GB per repository)
- Images are shared across all test configurations for the same distribution
- Requires the cluster to be running when saving/restoring images
- Uses `ctr` CLI which must be available in the cluster containers

## Technical Details

### Containerd Namespace

The action uses the `k8s.io` namespace in containerd, which is where Kubernetes stores container images.

### Container Name Detection

The action automatically detects the correct container name based on the distribution:

- **Kind (Vanilla)**: Uses name pattern `<cluster-name>-control-plane` (e.g., `kind-control-plane` for default cluster)
- **K3d (K3s)**: Uses name pattern `k3d-<cluster-name>-server-*` to find server containers. Note: K3d prefixes cluster names with `k3d-`, so a cluster named "k3d-default" will have containers like `k3d-k3d-default-server-0`.
- **Talos**: Uses label-based filtering with `talos.owned=true`, `talos.cluster.name=<cluster-name>`, and `talos.type=controlplane`. The resulting container names vary but typically follow patterns like `talos-<cluster-name>-controlplane-<number>`.

The action includes retry logic (up to 5 attempts with 2-second delays) to handle timing issues where containers may not be immediately discoverable after cluster creation.

### Error Handling

- Failed imports/exports are logged but don't fail the workflow
- Allows graceful degradation when cache is corrupted or incompatible
