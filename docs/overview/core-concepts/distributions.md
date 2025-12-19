---
title: "Distributions"
parent: Core Concepts
grand_parent: Overview
nav_order: 5
---

# Distributions

Distributions determine how Kubernetes is packaged and run. Select the distribution with `ksail cluster init --distribution` or set `spec.distribution` in `ksail.yaml`.

## Kind

[Kind](https://kind.sigs.k8s.io/) is the default distribution. It runs upstream Kubernetes inside Docker containers and mirrors production cluster behavior closely. LoadBalancer services can be accessed through host ports configured in `kind.yaml`.

## K3d

[K3d](https://k3d.io/) wraps the lightweight [K3s](https://k3s.io/) distribution in containers. It uses fewer resources while preserving core Kubernetes APIs. Configure host port mappings in `k3d.yaml` to reach LoadBalancer services from your workstation.
