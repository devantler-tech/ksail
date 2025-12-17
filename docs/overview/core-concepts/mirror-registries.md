# Mirror Registries

Mirror registries proxy upstream repositories (for example `docker.io`) and cache content close to your cluster. Configure mirrors with repeated `--mirror-registry <host>=<upstream>` flags during `ksail cluster init` or define them in `spec.registries.mirrors`.

> **Current limitations:**
>
> - Remote mirrors are not yet supported; KSail-Go always launches local `registry:3` containers.
> - Authentication to upstream registries is unsupported, so rate-limited public repositories may still require manual intervention.
> - TLS for upstream connections is planned but not currently wired through the CLI flags.

## Workflow Overview

1. Add mirrors (e.g., `ksail cluster init --mirror-registry docker.io=https://registry-1.docker.io`).
2. Run `ksail cluster create`; mirror containers start alongside your cluster.
3. Push images or let controllers pull through the mirror hosts you defined.
4. Delete the cluster with `ksail cluster delete --delete-volumes` to clean up persistent cache data.

Mirrors pair well with the [Local Registry](./local-registry.md) concept, keeping your development clusters responsive even with flaky internet connections.
