# Container Engines

KSail-Go provisions clusters on top of a container engine. You can choose the engine during initialization with `ksail cluster init --container-engine` or by setting `spec.runtime.containerEngine` in `ksail.yaml`.

## Docker

[Docker](https://www.docker.com/) remains the default engine. It integrates with the Kind and K3d distributions out of the box and works across macOS, Linux, and Windows. Stick with Docker when you want the most battle-tested workflow or rely on Docker Desktop's Kubernetes-adjacent tooling.

## Podman

[Podman](https://podman.io/) is a daemonless engine and a drop-in replacement for many Docker CLIs. KSail-Go supports Podman on Linux, macOS (via Podman Desktop), and Windows (through WSL). Use Podman if you prefer rootless containers or need tighter SELinux/AppArmor integration while keeping the same `ksail cluster` experience.
