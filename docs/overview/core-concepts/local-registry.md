---
title: Local Registry
parent: Core Concepts
grand_parent: Overview
nav_order: 8
---

KSail can run a local [OCI Distribution](https://distribution.github.io/distribution/) container to store images. Enable it with `--local-registry Enabled` during `ksail cluster init` or set `spec.localRegistry: Enabled` in `ksail.yaml`.

## Why Use a Local Registry?

- **Faster dev loops:** Push locally built images with `docker push localhost:<port>/<image>` and reference them in your manifests
- **GitOps integration:** Controllers can pull from the local registry like they would in production
- **Testing:** Validate image pull policies and registry behavior locally

## How It Works

1. **Initialization:** `ksail cluster init --local-registry Enabled --local-registry-port 5111` writes configuration to `ksail.yaml`
2. **Creation:** `ksail cluster create` starts a `registry:2` container and connects it to the cluster network
3. **Use:** Tag images with the registry host (e.g., `docker tag my-api localhost:5111/my-api`) and push
4. **Cleanup:** `ksail cluster delete` tears down the registry container

## Configuration

The local registry listens on port `5111` by default. Change it with `--local-registry-port`:

```bash
ksail cluster init --local-registry Enabled --local-registry-port 5000
```

## Troubleshooting

- **Registry container fails to start:** Check if the host port is already in use
- **Push requires authentication:** The local registry currently runs without authentication
