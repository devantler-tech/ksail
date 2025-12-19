---
title: CNI
parent: Core Concepts
grand_parent: Overview
nav_order: 1
---

The Container Network Interface determines how pods receive IP addresses and communicate inside your cluster. KSail exposes CNI selection declaratively via `spec.cni` in `ksail.yaml` or with `ksail cluster init --cni`.

## Available Options

### `Default`

Uses the distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d). Choose this for quick local iterations and CI environments.

### `Cilium`

Installs [Cilium](https://cilium.io/) through Helm. Pick Cilium when you need advanced observability, eBPF-based policies, or WireGuard encryption.

### `None`

Skips CNI installation entirely. Use this when you want to install a different CNI manually.

> **Tip:** The init command writes your selection to `ksail.yaml`. Future runs of `ksail cluster create` use that configuration.
