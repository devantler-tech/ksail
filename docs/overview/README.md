# Overview

KSail-Go is the next generation of the KSail developer experience—rewritten in Go and designed to give operators and developers a single CLI for cluster and workload lifecycle management. The tool wraps trusted Kubernetes utilities behind consistent commands so local development, CI validation, and GitOps workflows share the same interface.

![KSail Architecture](../images/architecture.drawio.png)

## Who Uses KSail-Go?

KSail-Go is built for platform engineers, site reliability engineers, and developers who want a fast feedback loop when working with Kubernetes. Because the CLI hides provider differences behind a shared contract, it is also approachable for engineers learning Kubernetes for the first time.

## What You Can Do with KSail-Go

- **Scaffold projects** – `ksail cluster init` creates a ready-to-run repo with declarative configuration, Kustomize scaffolding, and optional GitOps wiring.
- **Create and steward clusters** – Use the `ksail cluster` subcommands (`create`, `start`, `stop`, `delete`, `info`, `list`, `connect`) to manage Kind or K3d environments from one binary.
- **Reconcile workloads** – `ksail workload reconcile` integrates with Flux-aligned GitOps layouts so you can sync applications without leaving the terminal.
- **Generate manifests on demand** – The `ksail workload gen` namespace mirrors `kubectl create --dry-run` for common resources and Helm releases.
- **Manage encrypted assets** – `ksail cipher` wraps SOPS-based encryption, decryption, and editing flows for secrets stored in Git.
- **Validate as you iterate** – Workload commands provide guardrails for cluster assets, ensuring your configuration is valid before deployment.

## Navigating the Documentation

- [Project structure](project-structure.md) explains how the repository scaffolding is organized and how Kustomize overlays fit into the workflow.
- [Support matrix](support-matrix.md) lists the combinations we currently validate for platforms, container engines, and controllers.
- [Configuration guides](../configuration/) describe the CLI flags, declarative YAML, and precedence rules that shape each cluster.
- [Use-case playbooks](../use-cases/) capture guided workflows for local development, learning Kubernetes, and running KSail-Go inside CI pipelines.

Each section references the Go-based CLI and links back to the commands or configuration files you will touch most frequently.
