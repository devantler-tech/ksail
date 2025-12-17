# Distributions

Distributions determine how Kubernetes control plane components are packaged. Select the distribution when running `ksail cluster init --distribution` or set `spec.cluster.distribution` in `ksail.yaml`.

## Kind

[Kind](https://kind.sigs.k8s.io/) is the default distribution. It runs upstream Kubernetes inside Docker or Podman containers and mirrors the behavior of production clusters closely. KSail-Go provisions `cloud-provider-kind` sidecars so that LoadBalancer services acquire host ports automatically. On macOS and Windows, you can access those services through `localhost:<mapped-port>`.

## K3d

[K3d](https://k3d.io/) wraps the lightweight [K3s](https://k3s.io/) distribution in containers. It lowers resource usage while preserving core APIs. K3d natively exposes LoadBalancer services; configure host port mappings in `k3d.yaml` or pass `--k3d-port` flags during `ksail cluster init` to reach them from your workstation.
