---
title: Gateway Controllers
parent: Core Concepts
nav_order: 5
---

# Gateway Controllers

> [!NOTE]
> The [Gateway API](https://gateway-api.sigs.k8s.io) is a fairly new API that is designed to supercede the Ingress API. It solves some of the limitations of the Ingress API, but it is not yet widely adopted, and may have limited support in the implementation you are using.

`Gateway Controllers` refer to the controllers that manage gateway resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster. The `Gateway Controller` is responsible for managing the gateway resources and providing a way to route external traffic to the appropriate services.

## Default

The `Default` option is used when you want to use the default `Gateway Controller` that is bundled with the Kubernetes distribution you are using. Below is a table of the default `Gateway Controllers` for each Kubernetes distribution supported by KSail:

| Distribution | Gateway Controller |
| ------------ | ------------------ |
| kind         | None               |
| k3d          | None               |

## None

The `None` option is used when you do not want to use a `Gateway Controller`. In cases where a distribution installs a `Gateway Controller` by default, this option can be used to disable it.
