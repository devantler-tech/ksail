---
title: Ingress Controllers
parent: Core Concepts
layout: default
nav_order: 4
---

# Ingress Controllers

`Ingress Controllers` refer to the controllers that manage ingress resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster. The `Ingress Controller` is responsible for managing the ingress resources and providing a way to route external traffic to the appropriate services.

## None

If you choose `None`, no Ingress Controller will be installed.

## Default

| Provider | Distribution  | Ingress Controller |
| -------- | ------------- | ------------------ |
| Docker   | Native (kind) | None               |
| Docker   | K3s (k3d)     | Traefik            |
| Podman   | Native (kind) | None               |
| Podman   | K3s (k3d)     | Traefik            |
