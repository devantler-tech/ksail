# Local Registry

KSail-Go can run local [OCI Distribution](https://distribution.github.io/distribution/) containers to store images and cache upstream content. Mirror registries launch automatically when you pass one or more `--mirror-registry` flags to `ksail cluster init` or add entries to `spec.registries.mirrors` in `ksail.yaml`.

> **Limitations:** Remote registries cannot yet be reused as mirrors, and anonymous pulls from upstream registries remain unsupported. Authentication support is on the roadmap.

## Why Use a Local Registry?

- **Faster dev loops:** Push locally built images with `docker push localhost:<port>/<image>` and reference them in your manifests.
- **Offline resilience:** Mirror upstream repositories such as `docker.io` and continue to work when the public registry is rate limited or unavailable.
- **GitOps parity:** Flux and other controllers pull from the local registry exactly like they would in production.

## How It Works

1. **Initialization:** `ksail cluster init --mirror-registry docker.io=https://registry-1.docker.io` writes registry definitions into `ksail.yaml` and the generated distribution configs.
2. **Creation:** `ksail cluster create` starts `registry:3` containers for each mirror and connects them to the cluster network.
3. **Use:** Tag images with the mirror host (for example `docker tag my-api localhost:5001/my-api`) and push. Containerd inside the cluster is pre-configured to pull through the mirror.
4. **Cleanup:** `ksail cluster delete --delete-volumes` tears down mirror containers and their storage.

## Troubleshooting

- **Image pulls still hit upstream:** Confirm pods reference the mirror host (e.g., `localhost:5001/namespace/image`).
- **Registry container fails to start:** Check if the host port is already in use; update the port in `ksail.yaml` and rerun `ksail cluster create`.
- **Push requires authentication:** KSail-Go currently exposes mirrors without authentication; ensure your Docker client allows insecure registries when using HTTP.
