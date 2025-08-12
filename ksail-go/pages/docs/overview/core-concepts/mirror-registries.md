---
title: Mirror Registries
parent: Core Concepts
nav_order: 10
---

# Mirror Registries

> [!WARNING]
> Remote `Mirror Registries` are not supported yet. This means that remote registries cannot be used as mirrors for upstream registries.
>
> Support for unauthenticated access to upstream registries is also unsupported. This means that you cannot setup authentication in front of the mirror registry, or to authenticate from the mirror registry to the upstream registry.
>
> Lastly, mirror registries do not support secure connections to upstream registries with TLS.
>
> These are limitations of the current implementation and will be fixed in the future.

`Mirror Registries` refer to registries that are used to proxy and cache images from upstream registries. This is used to avoid pull rate limits and to speed up image pulls.

Using `Mirror Registries` will create a `registry:3` container for each mirror registry that is configured. You can configure as many mirror registries as you need.
