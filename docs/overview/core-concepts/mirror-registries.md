---
parent: Core Concepts
grand_parent: Overview
nav_order: 10
---

# Mirror Registries

Mirror registries proxy upstream container registries (e.g., `docker.io`) and cache content locally. Configure mirrors with `--mirror-registry <host>=<upstream>` flags during `ksail cluster init`.

## Workflow Overview

1. Add mirrors during initialization:
   ```bash
   ksail cluster init --mirror-registry docker.io=https://registry-1.docker.io
   ```
2. Run `ksail cluster create` to start mirror containers alongside your cluster
3. Images pulled from the mirrored registries will be cached locally
4. Delete the cluster with `ksail cluster delete --delete-volumes` to clean up cache data

## Configuration

Example mirror configuration in init:

```bash
ksail cluster init \
  --mirror-registry docker.io=https://registry-1.docker.io \
  --mirror-registry gcr.io=https://gcr.io
```

This creates local mirror containers that cache content from the upstream registries.

## Current Limitations

- Authentication to upstream registries is not yet fully supported
- TLS configuration for upstream connections is being developed
- Mirrors are always provisioned as local containers

## Use Cases

- **Rate limit avoidance:** Cache frequently pulled images to avoid Docker Hub rate limits
- **Offline development:** Work with previously pulled images when disconnected
- **CI/CD pipelines:** Speed up image pulls in automated testing
