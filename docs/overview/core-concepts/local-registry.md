---
title: Local Registry
parent: Core Concepts
nav_order: 9
---

# Local Registry

> [!WARNING]
> Using remote registries as a local registry is not supported yet. This means that remote registries cannot be used in place of a local registry for pushing and storing images.
>
> Support for unauthenticated access to upstream registries is also unsupported. This means that you cannot setup authentication in front of the local registry.
>
> These are limitations of the current implementation and will be fixed in the future.

`Local Registry` refers to the registry that is used to push and store OCI and Docker images. The primary use case for the `Local Registry` is to store OCI artifacts with manifests for GitOps based deployment tools, but it also allows you to push and store local images if you want to test out custom Docker images in Kubernetes, which are not available in upstream registries.

Using a `Local Registry` will create an official `registry:3` container in a specified container engine. The registry is configured to be accessible on `localhost`.
