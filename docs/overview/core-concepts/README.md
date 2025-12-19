---
title: "Core Concepts"
parent: Overview
nav_order: 3
has_children: true
---

# Core Concepts

This section explains the building blocks behind KSail. Each page links configuration values in `ksail.yaml` with CLI flags exposed by `ksail cluster` commands.

- [Container Network Interfaces](./cnis.html) — Choose networking providers such as the default distribution CNI or Cilium
- [Cert-Manager](./cert-manager.html) — Optionally install cert-manager for TLS certificates
- [Container Engines](./container-engines.html) — Docker support for running clusters
- [Container Storage Interfaces](./csis.html) — Configure persistent volume backends (planned feature)
- [Distributions](./distributions.html) — Compare Kind and K3d and how to choose between them
- [Gateway Controllers](./gateway-controllers.html) — Gateway API support (planned feature)
- [Ingress Controllers](./ingress-controllers.html) — Ingress configuration (planned feature)
- [Local Registry](./local-registry.html) — Run a local OCI registry for image storage
- [Metrics Server](./metrics-server.html) — Toggle cluster resource metrics for HPA and dashboards
- [Mirror Registries](./mirror-registries.html) — Configure registry mirrors for caching upstream content
- [Secret Manager](./secret-manager.html) — Encrypt files with SOPS using the `ksail cipher` commands

Each topic includes information about declarative fields (`spec.*`) and matching CLI arguments.
