---
title: Core Concepts
parent: Overview
nav_order: 3
has_children: true
---

This section explains the building blocks behind KSail. Each page links configuration values in `ksail.yaml` with CLI flags exposed by `ksail cluster` commands.

- [Container Network Interfaces](./cnis.md) — Choose networking providers such as the default distribution CNI or Cilium
- [Cert-Manager](./cert-manager.md) — Optionally install cert-manager for TLS certificates
- [Container Engines](./container-engines.md) — Docker support for running clusters
- [Container Storage Interfaces](./csis.md) — Configure persistent volume backends (planned feature)
- [Distributions](./distributions.md) — Compare Kind and K3d and how to choose between them
- [Gateway Controllers](./gateway-controllers.md) — Gateway API support (planned feature)
- [Ingress Controllers](./ingress-controllers.md) — Ingress configuration (planned feature)
- [Local Registry](./local-registry.md) — Run a local OCI registry for image storage
- [Metrics Server](./metrics-server.md) — Toggle cluster resource metrics for HPA and dashboards
- [Mirror Registries](./mirror-registries.md) — Configure registry mirrors for caching upstream content
- [Secret Manager](./secret-manager.md) — Encrypt files with SOPS using the `ksail cipher` commands

Each topic includes information about declarative fields (`spec.*`) and matching CLI arguments.
