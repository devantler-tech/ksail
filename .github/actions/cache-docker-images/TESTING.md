# Testing the Containerd Image Cache

This document describes how to test the containerd image caching functionality locally and in CI.

## Local Testing

### Prerequisites

- Docker installed and running
- KSail binary built (`go build -o ksail`)
- Access to the repository

### Test Steps

1. **Create a test cluster:**

   ```bash
   ./ksail cluster init --distribution Vanilla
   ./ksail cluster create
   ```

2. **Verify the cluster container exists:**

   ```bash
   docker ps --filter "name=kind-control-plane"
   ```

   Expected: Should show the Kind control plane container

3. **Apply some deployments to pull images:**

   ```bash
   ./ksail workload apply -k .github/fixtures/podinfo-overlay
   ```

4. **Manually test the export function:**

   ```bash
   # Get the container name
   CONTAINER_NAME=$(docker ps --filter "name=kind-control-plane" --format "{{.Names}}" | head -1)
   echo "Container: $CONTAINER_NAME"
   
   # List images in containerd
   docker exec "$CONTAINER_NAME" ctr -n k8s.io images ls
   
   # Export images
   docker exec "$CONTAINER_NAME" sh -c "ctr -n k8s.io images export /tmp/images.tar \$(ctr -n k8s.io images ls -q)"
   
   # Copy to host
   mkdir -p /tmp/test-cache
   docker cp "$CONTAINER_NAME":/tmp/images.tar /tmp/test-cache/images.tar
   
   # Check file size
   ls -lh /tmp/test-cache/images.tar
   ```

   Expected: Should create a tar file with exported images

5. **Test the import function:**

   ```bash
   # Delete the cluster
   ./ksail cluster delete
   
   # Create a fresh cluster
   ./ksail cluster create
   
   # Get new container name
   CONTAINER_NAME=$(docker ps --filter "name=kind-control-plane" --format "{{.Names}}" | head -1)
   
   # Copy cached images into container
   docker cp /tmp/test-cache/images.tar "$CONTAINER_NAME":/tmp/images.tar
   
   # Import images
   docker exec "$CONTAINER_NAME" ctr -n k8s.io images import /tmp/images.tar
   
   # Verify images are imported
   docker exec "$CONTAINER_NAME" ctr -n k8s.io images ls
   ```

   Expected: Should show the previously cached images

6. **Clean up:**

   ```bash
   ./ksail cluster delete
   rm -rf /tmp/test-cache
   ```

## CI Testing

The caching is automatically tested in the CI workflow during system tests. To verify it's working:

1. **Check the first run (cache miss):**
   - Look for `cache-hit: false` in the restore step
   - Verify images are pulled from registries
   - Check that the save step completes successfully

2. **Check subsequent runs (cache hit):**
   - Look for `cache-hit: true` in the restore step
   - Verify images are loaded from cache
   - Check that deployments complete faster

3. **Monitor for rate limiting errors:**
   - Before the change: Look for 429 errors in logs
   - After the change: Should see fewer or no rate limiting errors

## Troubleshooting

### Container not found

- Verify cluster is running: `docker ps`
- Check cluster name matches: compare with `--name` flag or default
- For K3d: Container names include `k3d-` prefix
- For Talos: Use label filters instead of name prefix

### Import fails

- Check `ctr` is available in the container: `docker exec <container> which ctr`
- Verify namespace is correct: should be `k8s.io`
- Check tar file integrity: `tar -tzf /tmp/containerd-cache/images.tar | head`

### Export fails

- Ensure images exist: `docker exec <container> ctr -n k8s.io images ls`
- Check disk space in container: `docker exec <container> df -h`
- Verify permissions: `docker exec <container> ls -la /tmp/`

### Cache not working in CI

- Check cache key consistency across runs
- Verify GitHub Actions cache limits (10GB max)
- Look for cache eviction messages
- Check if cache was invalidated (version bump)

## Expected Behavior

### First Run (Cold Cache)

1. Restore step: cache miss
2. Cluster created with empty containerd
3. Deployments pull images from registries (may hit rate limits)
4. Save step: exports all images to cache (several GB)
5. Cache saved for future runs

### Subsequent Runs (Warm Cache)

1. Restore step: cache hit
2. Cluster created with empty containerd
3. Cache images imported into containerd
4. Deployments use cached images (no registry pulls)
5. Save step: updates cache with any new images
6. Much faster execution, no rate limits

## Performance Metrics

Typical cache sizes:

- Vanilla (Kind): ~2-4 GB (depends on addons)
- K3s: ~1-3 GB (lighter than Kind)
- Talos: ~2-4 GB

Expected speedup:

- First run: +30s (cache export overhead)
- Subsequent runs: -2 to -5 minutes (no image pulls)

Network savings:

- Avoids pulling ~2-4 GB of images per test run
- Prevents 429 rate limiting errors
- Reduces load on public registries
