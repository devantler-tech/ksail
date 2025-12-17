# Core Concepts

This section explains the building blocks behind KSail-Go. Each page links configuration values in `ksail.yaml` with the CLI flags exposed by `ksail cluster` and `ksail workload`.

- [Container Network Interfaces](./cnis.md) — Choose networking providers such as the default distribution CNI or Cilium when you run `ksail cluster init --cni`.
- [Cert-Manager](./cert-manager.md) — Optionally install cert-manager for TLS certificates and issuers.
- [Container Engines](./container-engines.md) — Understand how Docker and Podman influence cluster lifecycle commands.
- [Container Storage Interfaces](./csis.md) — Configure persistent volume backends for your clusters.
- [Deployment Tools](./deployment-tools.md) — Learn how Flux and kubectl integrate with KSail-Go workloads.
- [Distributions](./distributions.md) — Compare Kind and K3d behaviors and how to switch between them.
- [Editors](./editor.md) — Control the editor used by interactive commands such as `ksail cipher edit`.
- [Gateway Controllers](./gateway-controllers.md) — Discover options for Gateway API enablement.
- [Ingress Controllers](./ingress-controllers.md) — Select ingress implementations via declarative config or CLI flags.
- [Local Registry](./local-registry.md) — Mirror container images closer to development clusters.
- [Metrics Server](./metrics-server.md) — Toggle cluster resource metrics for HPA and dashboards.
- [Mirror Registries](./mirror-registries.md) — Configure and sync upstream registries for air-gapped workflows.

- [Secret Manager](./secret-manager.md) — Encrypt workloads with SOPS using the `ksail cipher` commands.

Each topic includes callouts for declarative fields (`spec.*`) and the matching CLI arguments so you can choose whichever interface suits your workflow.
