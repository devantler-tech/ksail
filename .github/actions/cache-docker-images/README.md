# Cache Docker Images

This composite action caches Docker images used by Kind, K3d, and Talos to avoid rate limiting errors when pulling images during CI workflows.

## Purpose

The system tests in the CI workflow frequently pull Docker images for different Kubernetes distributions:
- **Vanilla (Kind)**: Uses `kindest/node` images from Docker Hub
- **K3s (K3d)**: Uses `rancher/k3s` images from Docker Hub  
- **Talos**: Uses `ghcr.io/siderolabs/talos` and `ghcr.io/siderolabs/installer` images from GHCR

Without caching, each workflow run pulls these images fresh, which can lead to:
- Rate limiting errors (429 responses) from container registries
- Slower workflow execution times
- Unreliable CI runs

This action solves these issues by:
1. Saving pulled images to tar archives
2. Caching the archives using GitHub Actions cache
3. Restoring and loading images on subsequent runs

## Inputs

### `distribution` (required)
Kubernetes distribution type. Determines which images to cache.
- **Vanilla**: Caches Kind images (`kindest/node`)
- **K3s**: Caches K3d images (`rancher/k3s`)
- **Talos**: Caches Talos images (`ghcr.io/siderolabs/talos`, `ghcr.io/siderolabs/installer`)

### `cache-key-suffix` (optional)
Optional suffix for the cache key. Useful for versioning or invalidating the cache.
- Default: `""`

## Outputs

### `cache-hit`
Whether the cache was hit (`'true'` or `'false'`).

## Usage

```yaml
- name: 📦 Cache Docker Images
  uses: ./.github/actions/cache-docker-images
  with:
    distribution: Vanilla
```

## Example Integration

```yaml
jobs:
  system-test:
    runs-on: ubuntu-latest
    steps:
      - name: 📄 Checkout
        uses: actions/checkout@v6
      
      - name: 📦 Cache Docker Images
        uses: ./.github/actions/cache-docker-images
        with:
          distribution: ${{ matrix.distribution }}
      
      - name: 🧪 Run system tests
        run: ksail cluster create --distribution ${{ matrix.distribution }}
```

## How It Works

1. **Generate cache key**: Creates a unique key based on distribution type (e.g., `docker-images-kind-v1`)
2. **Determine images**: Identifies which images to cache based on the distribution
3. **Restore cache**: Attempts to restore cached images from previous runs
4. **Load or pull**: 
   - If cache hit: Loads images from tar archives
   - If cache miss: Pulls images from registries and saves to tar archives
5. **Save cache**: Saves the tar archives for future runs (only on cache miss)

## Cache Invalidation

To invalidate the cache and force a fresh pull of images:
1. Increment the version in the cache key (e.g., `v1` → `v2`)
2. Or provide a different `cache-key-suffix`

## Limitations

- Cache size is limited by GitHub Actions cache limits (10GB per repository)
- Only caches a predefined set of common image versions
- Images must be publicly accessible (or credentials must be configured separately)
