---
title: Ingress Controllers
parent: Core Concepts
nav_order: 4
---

# Ingress Controllers

`Ingress Controllers` refer to the controllers that manage ingress resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster. Below is a table of the of default `Ingress Controllers` on each Kubernetes distribution supported by KSail:

## Default

| Distribution | Ingress Controller |
| ------------ | ------------------ |
| kind         | None               |
| k3d          | Traefik            |

## Traefik

If you choose [`Traefik`](https://github.com/traefik/traefik-helm-chart), the Traefik Ingress Controller will be installed in your cluster. Traefik is a popular open-source ingress controller that provides advanced routing capabilities, including support for dynamic configuration, load balancing, and SSL termination.

## None

The `None` Ingress Controller option means that no Ingress Controller will be installed in your Kubernetes cluster. This is useful if you do not need an ingress controller or if you plan to use an unsupported ingress controller.
