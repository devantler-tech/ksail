---
title: Providers
parent: Core Concepts
layout: default
nav_order: 0
---

# Providers

> [!NOTE]
> KSail is designed to be extensible and support multiple providers. However, at the moment, only the `Docker` provider is supported.

`Providers` refer to the underlying infrastructure that is used to create the Kubernetes cluster. This can be a local provider, on-premises provider or a cloud provider. The `Provider` is responsible for creating and managing the underlying infrastructure that is used to run the Kubernetes cluster.

## [Docker](https://www.docker.com/)

The `Docker` provider is a container runtime that is used to create and manage containers. It is the most widely used container runtime and it is supported on all major operating systems.

Using `Docker` as a provider will create a Kubernetes cluster as container(s).

## [Podman](https://podman.io/)

The `Podman` provider is a container runtime that is used to create and manage containers. It is a daemonless container runtime that is compatible with Docker. It is supported on all major operating systems.

Using `Podman` as a provider will create a Kubernetes cluster as container(s).
