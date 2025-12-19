---
title: "Ingress Controllers"
parent: Core Concepts
grand_parent: Overview
nav_order: 7
---

# Ingress Controllers

Ingress controllers expose HTTP(S) services from inside the cluster.

> **Note:** Ingress controller configuration is not yet implemented in KSail. This document describes planned functionality.

## Planned Options

### Default

The `Default` option will use the controller bundled with the distribution. Kind does not install an ingress controller by default, while K3d includes Traefik.

### Traefik

Installing Traefik explicitly will ensure it's available regardless of distribution defaults.

### None

Skips ingress installation. Useful for testing headless services or deploying alternative controllers manually.

## Current Status

Configure ingress through your distribution's configuration file (`kind.yaml` or `k3d.yaml`) for now.
